package main

import (
    "fmt"
    "net/http"
    "encoding/json"
    "os"
    "os/exec"
    "strings"
    "log"
    "errors"
    "strconv"
    "golang.org/x/net/context"
    "google.golang.org/grpc"
    pb "github.com/cookinrelaxin/service_updater/protocol"
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
* TODO: Rewrite the code to use log instead of fmt.
*/

/*
* TODO: Connect to the `service_updater` service to send service update messages upon successfully building a new Docker image. 
*/

/*
* Reference:
* https://developer.github.com/v3/activity/events/types/#pushevent
*/

const (
    repositoriesDir string = "/tmp/cloned_repositories/"
    updaterHostname string = "service_updater:8080"
)

var (
    dockerHubUsername string
    dockerHubPassword string

    githubUsername string
    githubPassword string
)

/*
 * Verify that the required environement variables exist.
 */
func init() {

    dockerHubUsername := os.Getenv("DOCKER_HUB_USERNAME")
    dockerHubPassword := os.Getenv("DOCKER_HUB_PASSWORD")
    githubUsername := os.Getenv("GITHUB_USERNAME")
    githubPassword := os.Getenv("GITHUB_PASSWORD")

    switch {
    case dockerHubUsername == "":
        fmt.Println("Error: environement variable DOCKER_HUB_USERNAME required.")
        os.Exit(1)
    case dockerHubPassword == "":
        fmt.Println("Error: environement variable DOCKER_HUB_PASSWORD required.")
        os.Exit(1)
    case githubUsername == "":
        fmt.Println("Error: environment variable GITHUB_USERNAME required.")
        os.Exit(1)
    case githubPassword == "":
        fmt.Println("Error: environment variable GITHUB_PASSWORD required.")
        os.Exit(1)
    }
}


/*
* Clone the specified Git repository to disk.
*/
func cloneRepository(repo Repository) (repositoryPath string, err error) {
    /*
    * Verify that the repo struct is valid.
    */
    switch {
    case repo.Name == "":
        return repositoryPath, errors.New("repo.Name not specified")
    case repo.URL == "":
        return repositoryPath, errors.New("repo.URL not specified")
    }

    /*
    * Verify that `cloned_repositories` exists.
    */
    if _, err := os.Stat(repositoriesDir); err != nil {
        if os.IsNotExist(err) {
            log.Printf("Directory %s does not exist. Attempting to create.", repositoriesDir)
            if err := os.Mkdir(repositoriesDir, 0755); err != nil {
                return repositoryPath, err
            }
            log.Printf("Sucessfully created %s.", repositoriesDir)
        } else {
            return repositoryPath, err
        }
    } else {
        log.Printf("Directory %s exists. Proceeding.", repositoriesDir)
    }

    repositoryPath = repositoriesDir + repo.Name

    /*
    * Remove the to-be-cloned repository if it already is present.
    */
    if err := os.RemoveAll(repositoryPath); err != nil {
        return repositoryPath, err
    }

    authenticated_url := "https://"+githubUsername+":"+githubPassword+"@github.com/"+githubUsername+"/"+repo.Name
    log.Printf("Attempting to clone %s...\n", repo.URL)
    gitCmd := exec.Command("git", "clone", authenticated_url, repositoryPath)
    gitCmd.Stdout = os.Stdout
    gitCmd.Stderr = os.Stderr
    if err := gitCmd.Run(); err != nil {
        return repositoryPath, err
    }
    log.Printf("Successfully cloned %s.\n", repo.URL)

    return repositoryPath, nil
}

/*
* Get the `version number` of a microservice from the size of its Git rev-list.
*/
func getVersionNumber(repositoryPath string) (versionNumber int, err error) {
    log.Printf("Attemping to change into %s.\n", repositoryPath)
    if err := os.Chdir(repositoryPath); err != nil {
        return versionNumber, err
    }
    log.Printf("Successfully changed into %s.\n", repositoryPath)

    log.Printf("Attemping to obtain version number.\n")
    gitCmd := exec.Command("git", "rev-list", "--count", "--first-parent", "HEAD")
    gitOut, err := gitCmd.Output()
    if err != nil {
        return versionNumber, err
    }
    log.Printf("Successfully obtained version number.\n")
    versionNumber, err = strconv.Atoi(strings.TrimSuffix(string(gitOut), "\n"))
    return versionNumber, err
}

/*
* Log into Docker Hub.
*/
func dockerHubLogin(username, password string) error {
    log.Printf("Attempting to login to Docker Hub...\n")
    loginCmd := exec.Command("docker", "login", "-u", username, "-p", password)
    loginCmd.Stdout = os.Stdout
    loginCmd.Stderr = os.Stderr
    if err := loginCmd.Run(); err != nil {
        return err
    }
    fmt.Printf("Successfully logged in to Docker Hub...\n")
    return nil
}

/*
* Pull the specified Docker Image.
*/
func pullImage(imageName string) error {
    log.Printf("Attempting to pull image %s from Docker Hub...\n", imageName)
    pullCmd := exec.Command("docker", "pull", imageName)
    pullCmd.Stdout = os.Stdout
    pullCmd.Stderr = os.Stderr
    if err := pullCmd.Run(); err != nil {
        return err
    }
    log.Printf("Successfully pulled image.\n")
    return nil
}

/*
* Build an image from a Dockerfile.
*/
func buildImage(imageTag string) error {
    log.Printf("Attempting to build %s from Dockerfile...\n", imageTag)
    buildCmd := exec.Command("docker", "build", "-t", imageTag, ".")
    buildCmd.Stdout = os.Stdout
    buildCmd.Stderr = os.Stderr
    if err := buildCmd.Run(); err != nil {
        return err
    }
    log.Printf("Successfully built new Docker image.\n")
    return nil
}

/*
* Push the specified Docker Image.
*/
func pushImage(imageName string) error {
    log.Printf("Attempting to push %s to Docker Hub...\n", imageName)
    pushCmd := exec.Command("docker", "push", imageName)
    pushCmd.Stdout = os.Stdout
    pushCmd.Stderr = os.Stderr
    if err := pushCmd.Run(); err != nil {
        return err
    }
    fmt.Printf("Successfully pushed current Docker image.\n")
    return nil
}

type Repository struct {
    Name string `json:"name"`
    URL string `json:"URL"`
}

type PushEvent struct {
    Repository `json:"repository"`
}

func handler(w http.ResponseWriter, r *http.Request) {
    buf := make([]byte, r.ContentLength)
    n, _ := r.Body.Read(buf)
    var push_event PushEvent
    if err := json.Unmarshal(buf[:n], &push_event); err != nil {
        panic(err)
    }
    fmt.Printf("%+v\n", push_event)

    go func() {
        repositoryPath, err := cloneRepository(push_event.Repository)
        if err != nil {
            panic(err)
        }
        versionNumber, err := getVersionNumber(repositoryPath)
        if err != nil {
            panic(err)
        }
        fmt.Println(versionNumber)

        if err := dockerHubLogin(dockerHubUsername, dockerHubPassword); err != nil {
            panic(err)
        }

        currentImageName := dockerHubUsername + "/" + push_event.Repository.Name + ":latest"
        if err := pullImage(currentImageName); err != nil {
            panic(err)
        }

        newImageName := dockerHubUsername + "/" + push_event.Repository.Name + ":" + strconv.Itoa(versionNumber)
        if err := buildImage(newImageName); err != nil {
            panic(err)
        }

        if err := pushImage(newImageName); err != nil {
            panic(err)
        }

        conn, err := grpc.Dial(updaterHostname, grpc.WithInsecure())
        if err != nil {
            log.Fatalf("Could not connect to %s: %v", updaterHostname, err)
        }
        defer conn.Close()
        c := pb.NewServiceUpdaterClient(conn)

        serviceName := push_event.Repository.Name
        r, err := c.UpdateService(context.Background(), &pb.UpdateRequest{ServiceName: serviceName, ImageName: newImageName})
        if err != nil {
            log.Fatalf("could not update service: %v", err)
        }
        log.Printf("%#v", r)
    }()
}


func main() {
    http.HandleFunc("/", handler)
    http.ListenAndServe(":8080", nil)
}
