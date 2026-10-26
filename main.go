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

	"github.com/go-git/go-git/v5"
	"github.com/google/go-github/v93/github"
)

var (
	path       = "repos"
	githubUser = currentUser()
	slackURL   = ""
)

func main() {
	flag.StringVar(&path, "path", path, "path to store repos")
	flag.StringVar(&githubUser, "github-user", githubUser, "github user to download repos")
	flag.StringVar(&slackURL, "slack-url", slackURL, "Slack URL")
	flag.Parse()

	client, err := github.NewClient()
	if err != nil {
		log.Fatalf("Error creating GitHub client: %s", err)
	}

	for {
		backupAll(context.Background(), client)
		time.Sleep(time.Hour * 24)
	}
}

// backupAll clones or updates every repository owned by githubUser. Errors
// are reported and do not stop the remaining repositories (or later runs)
// from being backed up.
func backupAll(ctx context.Context, client *github.Client) {
	repos, err := githubRepos(ctx, client, githubUser)
	if err != nil {
		msg := fmt.Sprintf("Error getting repos for %s: %s\n", githubUser, err)
		slack(msg, slackURL)
		return
	}

	for _, repo := range repos {
		log.Printf("%s: %s\n", repo.GetName(), repo.GetCloneURL())

		dir := filepath.Join(path, repo.GetName())

		if repoExists(dir) {
			if err = update(dir); err != nil {
				msg := fmt.Sprintf("Error updating repo %s: %s", repo.GetName(), err)
				slack(msg, slackURL)
			}
		} else {
			if err = clone(dir, repo.GetCloneURL()); err != nil {
				msg := fmt.Sprintf("Error cloning repo %s: %s", repo.GetName(), err)
				slack(msg, slackURL)
			}
		}
	}
}

// githubRepos returns all of user's repositories, following pagination.
func githubRepos(ctx context.Context, client *github.Client, user string) ([]*github.Repository, error) {
	opts := &github.RepositoryListByUserOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var repos []*github.Repository
	for repo, err := range client.Repositories.ListByUserIter(ctx, user, opts) {
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, nil
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

// slack logs update and, if url is set, posts it to that Slack webhook.
func slack(update, url string) {
	log.Print(update)
	if url == "" { // if no url is supplied there is nothing more to do
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

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		log.Printf("Slack webhook returned unexpected status: %s\n", resp.Status)
	}
}

// Attempts to get the current user. If it can't get the current user it returns an empty string.
func currentUser() string {
	user, err := user.Current()
	if err != nil {
		return ""
	}

	return user.Username
}
