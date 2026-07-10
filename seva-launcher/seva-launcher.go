package main

import (
	"embed"
	"flag"
	"github.com/gorilla/mux"
	"github.com/skratchdot/open-golang/open"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

//latest-tag for seva-browser
var docker_browser_tag = "v1.0.0"
var docker_browser_path = "ghcr.io/texasinstruments/seva-browser:" + docker_browser_tag

// Link to Repository which has docker-compose file for each demo 
var store_url = "https://raw.githubusercontent.com/TexasInstruments/seva-apps/main"

var addr = flag.String("addr", "0.0.0.0:8007", "http service address")
var no_browser = flag.Bool("no-browser", false, "do not launch browser")
var docker_browser = flag.Bool("docker-browser", false, "force use of docker browser")
var http_proxy = flag.String("http_proxy", "", "use to set http proxy")
var no_proxy = flag.String("no_proxy", "", "use to set no-proxy")

var container_id_list [1]string
var docker_compose string

//go:embed web/*
var content embed.FS

//go:embed docker-compose
var docker_compose_bin []byte

func prepare_compose() string {
    // Try V2 first
    cmd := exec.Command("docker", "compose", "version")
    if err := cmd.Run(); err == nil {
        return "docker"
    }
    // Try V1
    cmd = exec.Command("docker-compose", "-v")
    if err := cmd.Run(); err == nil {
        return "docker-compose"
    }
    // Neither installed, use bundled binary
	ioutil.WriteFile("docker-compose", docker_compose_bin, 0755)
	return "./docker-compose"
}

func setup_working_directory() {
	err := os.MkdirAll("/tmp/seva-launcher", os.ModePerm)
	if err != nil {
		log.Println(err)
		exit(1)
	}
	err = os.Chdir("/tmp/seva-launcher")
	if err != nil {
		log.Println(err)
		exit(1)
	}
}

func launch_browser() {
	if *docker_browser {
		go launch_docker_browser()
	} else {
		err := open.Start("http://localhost:8007/#/")
		if err != nil {
			log.Println("Host browser not detected, trying to load & launch seva-browser packaged in default image")
			go launch_docker_browser()
		}
	}
}

// Launches seva-browser
func launch_docker_browser() {
	xdg_runtime_dir := os.Getenv("XDG_RUNTIME_DIR")

	output := docker_run("--rm", "--privileged", "--network", "host",
		"-e", "XDG_RUNTIME_DIR=/tmp",
                "-e", "DISPLAY",
		"-e", "WAYLAND_DISPLAY",
		"-e", "https_proxy",
		"-e", "http_proxy",
		"-e", "no_proxy",
		"-v", xdg_runtime_dir+":/tmp",
		"-u", "user",
		"ghcr.io/texasinstruments/seva-browser:"+docker_browser_tag,
		"http://localhost:8007/#/",
	)
	output_strings := strings.Split(strings.TrimSpace(string(output)), "\n")
	container_id_list[0] = output_strings[len(output_strings)-1]
}

func docker_run(args ...string) []byte {
	args = append([]string{"run", "-d"}, args...)
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	log.Printf("|\n%s\n", output)
	if err != nil {
		log.Println("Failed to start container!")
		log.Println(err)
		exit(1)
	}
	return output
}

func exit(num int) {
	log.Println("Stopping non-app containers")
	for _, container_id := range container_id_list {
		if len(container_id) > 0 {
			cmd := exec.Command("docker", "stop", container_id)
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("Failed to stop container %s : \n%s", container_id, output)
			}
		}
	}
	os.Exit(num)
}

func setup_exit_handler() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		exit(0)
	}()
}

func handle_requests() {
	router := mux.NewRouter()
	router.HandleFunc("/ws", websocket_controller)
	log.Println("Listening for websocket messages at " + *addr + "/ws")
	root_content, err := fs.Sub(content, "web")
	if err != nil {
		log.Println("No files to server for web interface!")
		exit(1)
	}
	router.PathPrefix("/").Handler(http.FileServer(http.FS(root_content)))
	log.Println(http.ListenAndServe(*addr, router))
}

func check_env_vars() {
	for _, element := range []string{"DISPLAY", "WAYLAND_DISPLAY"} {
		env_var := os.Getenv(element)
		if len(env_var) > 0 {
			return
		}
	}
	log.Println("Environment variable DISPLAY or WAYLAND_DISPLAY must be set!")
	exit(1)
}

func valid_proxy() bool {
	_, err := url.ParseRequestURI(*http_proxy)
	return err == nil
}

func setup_proxy() {
	// Setting up Environment Variables
	// If http_proxy is valid apply changes to Environment variable
	if *http_proxy == "" && *no_proxy == "" {
		// TODO: Revert proxy settings
	} else if valid_proxy() {
		proxy_settings := ProxySettings{
			HTTPS: *http_proxy,
			HTTP:  *http_proxy,
			FTP:   *http_proxy,
			NO:    *no_proxy,
		}
		apply_proxy_settings(proxy_settings)
	} else {
		log.Println("Invalid proxy given, ignoring proxy settings!")
	}
}

func main() {
	setup_exit_handler()
	check_env_vars()
	flag.Parse()

	setup_proxy()

	log.Println("Setting up working directory")
	setup_working_directory()
	docker_compose = prepare_compose()

	if !*no_browser {
		log.Println("Launching browser")
		launch_browser()
	}

	handle_requests()
}
