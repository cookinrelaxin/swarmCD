package main

import (
    "fmt"
    "net/http"
    "encoding/json"
    "io/ioutil"
    "os"
    "os/exec"
    "strings"
    "flag"
)

/*
* We want to:
* 
* - Listen for POSTs from Github notifying about code pushes
* - Pull the relevant repository from Github
* - Build a new Docker container from the code, and tag it with a new version number
* - Use `git rev-list --count --first-parent HEAD` to get the new version number
* - Push the new container to Docker Hub (registry)
* - Tell docker engine to update the corresponding service 
* - That's it.
* 
* We can test it by mocking Github's webhooks
*/

/*
* Reference:
* https://developer.github.com/v3/activity/events/types/#pushevent
* https://eager.io/blog/go-and-json/
*/

var dockerHubUsername string
var dockerHubPassword string

var githubUsername string
var githubPassword string

func cloneRepository(name, url string) {

    defer func() {
        if err := os.Chdir("../"); err != nil {
            panic(err)
        }

        if err := os.RemoveAll(name); err != nil {
            panic(err)
        }
    }()

    authenticated_url := "https://"+githubUsername+":"+githubPassword+"@github.com/"+githubUsername+"/"+name
    fmt.Printf("Attempting to clone %s...\n", url)
    gitCmd := exec.Command("git", "clone", authenticated_url)
    gitCmd.Stdout = os.Stdout
    gitCmd.Stderr = os.Stderr
    if err := gitCmd.Run(); err != nil {
        panic(err)
    }
    fmt.Printf("Successfully cloned %s.\n", url)

    fmt.Printf("Attempting to change directory into %s...\n", name)
    if err := os.Chdir(name); err != nil {
        panic(err)
    }
    fmt.Printf("Successfully changed directory into %s.\n", name)

    fmt.Printf("Attempting to get a new version number...\n")
    version := getVersionNumber()
    fmt.Printf("Successfully retrieved a new version number: %s\n", version)

    fmt.Printf("Attempting to login to Docker Hub...\n")
    loginCmd := exec.Command("docker", "login", "-u", dockerHubUsername, "-p", dockerHubPassword)
    loginCmd.Stdout = os.Stdout
    loginCmd.Stderr = os.Stderr
    if err := loginCmd.Run(); err != nil {
        panic(err)
    }
    fmt.Printf("Successfully logged in to Docker Hub...\n")

    fmt.Printf("Attempting to pull current Docker image from Docker Hub...\n")
    pullCmd := exec.Command("docker", "pull", dockerHubUsername+"/"+name+":"+"latest")
    pullCmd.Stdout = os.Stdout
    pullCmd.Stderr = os.Stderr
    if err := pullCmd.Run(); err != nil {
        panic(err)
    }
    fmt.Printf("Successfully pulled current Docker image.\n")

    fmt.Printf("Attempting to build new Docker image from Dockerfile...\n")
    buildCmd := exec.Command("docker", "build", "-t", dockerHubUsername+"/"+name+":"+version, ".")
    buildCmd.Stdout = os.Stdout
    buildCmd.Stderr = os.Stderr
    if err := buildCmd.Run(); err != nil {
        panic(err)
    }
    fmt.Printf("Successfully built new Docker image.\n")

    fmt.Printf("Attempting to push new Docker image to Docker Hub...\n")
    pushCmd := exec.Command("docker", "push", dockerHubUsername+"/"+name+":"+version)
    pushCmd.Stdout = os.Stdout
    pushCmd.Stderr = os.Stderr
    if err := pushCmd.Run(); err != nil {
        panic(err)
    }
    fmt.Printf("Successfully pushed current Docker image.\n")

    fmt.Printf("Attempting to update the swarm-mode '%s' service to version %s...\n", name, version)
    updateCmd := exec.Command("docker", "service", "update", "--image", dockerHubUsername+"/"+name+":"+version, name)
    updateCmd.Stdout = os.Stdout
    updateCmd.Stderr = os.Stderr
    if err := updateCmd.Run(); err != nil {
        panic(err)
    }
    fmt.Printf("Successfully updated %s service.\n", name)

}

func getVersionNumber() string {
    gitCmd := exec.Command("git", "rev-list", "--count", "--first-parent", "HEAD")
    gitOut, err := gitCmd.Output()
    if err != nil {
        panic(err)
    }
    version := strings.TrimSuffix(string(gitOut), "\n")
    return version
}

type Repository struct {
    Name string `json:"name"`
    HTMLURL string `json:"html_url"`
}

type PushEvent struct {
    Repository `json:"repository"`
}

func handler(w http.ResponseWriter, r *http.Request) {
    body, err := ioutil.ReadAll(r.Body)
    if err != nil {
        panic(err)
    }
    var push_event PushEvent
    if err := json.Unmarshal(body, &push_event); err != nil {
        panic(err)
    }
    fmt.Printf("%#v\n", push_event)

    name := push_event.Repository.Name
    url := push_event.Repository.HTMLURL

    cloneRepository(name, url)
}


func main() {
    flag.StringVar(&dockerHubUsername, "docker-hub-username", "", "Specify your Docker Hub username")
    flag.StringVar(&dockerHubPassword, "docker-hub-password", "", "Specify your Docker Hub password")
    flag.StringVar(&githubUsername, "github-username", "", "Specify your Github username")
    flag.StringVar(&githubPassword, "github-password", "", "Specify your Github password")
    flag.Parse()

    if dockerHubUsername == "" {
        fmt.Println("Error: -docker-hub-username required. Set -h for instructions.")
        os.Exit(1)
    }

    if dockerHubPassword == "" {
        fmt.Println("Error: -docker-hub-password required. Set -h for instructions.")
        os.Exit(1)
    }

    if githubUsername == "" {
        fmt.Println("Error: -github-username required. Set -h for instructions.")
        os.Exit(1)
    }

    if githubPassword == "" {
        fmt.Println("Error: -github-password required. Set -h for instructions.")
        os.Exit(1)
    }

    http.HandleFunc("/", handler)
    http.ListenAndServe(":8080", nil)
}
