package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/google/go-github/github"
	"gopkg.in/src-d/go-git.v4"
)

var (
	path       = "repos"
	githubUser = currentUser()
	slackURL   = ""
)

func init() {
	flag.StringVar(&path, "path", path, "path to store repos")
	flag.StringVar(&githubUser, "github-user", githubUser, "github user to download repos")
	flag.StringVar(&slackURL, "slack-url", slackURL, "Slack URL")
	flag.Parse()
}

func main() {
	for {
		repos, err := githubRepos(githubUser)
		if err != nil {
			msg := fmt.Sprintf("Error getting repos for %s: %s\n", githubUser, err)
			slack(msg, slackURL)
			return
		}

		for _, repo := range repos {
			log.Printf("%s: %s\n", *repo.Name, *repo.CloneURL)

			dir := filepath.Join(path, *repo.Name)

			if repoExists(dir) {
				if err = update(dir); err != nil {
					msg := fmt.Sprintf("Error updating repo %s: %s", *repo.Name, err)
					slack(msg, slackURL)
				}
			} else {
				if err = clone(dir, *repo.CloneURL); err != nil {
					msg := fmt.Sprintf("Error cloning repo %s: %s", *repo.Name, err)
					slack(msg, slackURL)
				}
			}
		}

		time.Sleep(time.Hour * 24)
	}

}

func githubRepos(user string) ([]*github.Repository, error) {
	client := github.NewClient(nil)
	repos, _, err := client.Repositories.List(context.Background(), user, nil)

	return repos, err
}

func update(dir string) error {
	rep, err := git.PlainOpen(dir)
	if err != nil {
		return err
	}

	wt, err := rep.Worktree()
	if err != nil {
		return err
	}

	err = wt.Pull(&git.PullOptions{})
	if err == git.NoErrAlreadyUpToDate {
		return nil
	}

	return err
}

func clone(dir, repoURL string) error {
	opts := &git.CloneOptions{
		URL:      repoURL,
		Progress: os.Stdout,
	}

	_, err := git.PlainClone(dir, false, opts)
	return err
}

func repoExists(dir string) bool {
	_, err := os.Stat(dir)
	return !os.IsNotExist(err)
}

func slack(update, url string) {
	if url == "" { // if no url is supplied just return
		return
	}

	type Payload struct {
		Text      string `json:"text"`
		IconURL   string `json:"icon_url,omitempty"`
		IconEmoji string `json:"icon_emoji,omitempty"`
		Username  string `json:"username,omitempty"`
	}

	payload := &Payload{
		Username: "GITBACKUP",
		Text:     update,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error encoding slack Payload: %s\n", err)
		return
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Error creating new http request: %s\n", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error executing http request: %s\n", err)
		return
	}
	defer resp.Body.Close()

	return
}

// Attempts to get the current user. If it can't get the current user it returns an empty string.
func currentUser() string {
	user, err := user.Current()
	if err != nil {
		return ""
	}

	return user.Username
}
