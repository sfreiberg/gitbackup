package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/google/go-github/v93/github"
)

// commitFile writes name with content into the worktree of r and commits it.
func commitFile(t *testing.T, r *git.Repository, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatal(err)
	}
	_, err = wt.Commit("add "+name, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCloneAndUpdate(t *testing.T) {
	src := t.TempDir()
	srcRepo, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, srcRepo, src, "a.txt", "a")

	dst := filepath.Join(t.TempDir(), "backup")
	if repoExists(dst) {
		t.Fatal("repoExists reported a missing directory as existing")
	}
	if err := clone(dst, src); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if !repoExists(dst) {
		t.Fatal("repoExists reported a cloned repo as missing")
	}
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Fatalf("cloned file missing: %v", err)
	}

	// Nothing new upstream: update must treat "already up to date" as success.
	if err := update(dst); err != nil {
		t.Fatalf("update with no changes: %v", err)
	}

	// New upstream commit is pulled in.
	commitFile(t, srcRepo, src, "b.txt", "b")
	if err := update(dst); err != nil {
		t.Fatalf("update: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "b.txt")); err != nil || string(b) != "b" {
		t.Fatalf("pulled file: %q, %v", b, err)
	}
}

func TestUpdateNotARepo(t *testing.T) {
	if err := update(t.TempDir()); err == nil {
		t.Fatal("expected an error updating a directory that is not a git repo")
	}
}

func TestSlack(t *testing.T) {
	type payload struct {
		Text     string `json:"text"`
		Username string `json:"username"`
	}
	got := make(chan payload, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		var p payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decoding payload: %v", err)
		}
		got <- p
	}))
	defer srv.Close()

	slack(`error "cloning" repo`, srv.URL)
	select {
	case p := <-got:
		if p.Text != `error "cloning" repo` || p.Username != "GITBACKUP" {
			t.Errorf("unexpected payload: %+v", p)
		}
	default:
		t.Fatal("no request was sent to the slack URL")
	}
}

func TestSlackNoURL(t *testing.T) {
	slack("message", "") // must not panic or attempt a request
}

func TestGithubRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users/octo/repos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"name": "one", "clone_url": "https://github.com/octo/one.git"},
			{"name": "two", "clone_url": "https://github.com/octo/two.git"}
		]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	base := srv.URL + "/"
	client, err := github.NewClient(github.WithURLs(&base, &base))
	if err != nil {
		t.Fatal(err)
	}
	repos, err := githubRepos(context.Background(), client, "octo")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].GetName() != "one" || repos[1].GetCloneURL() != "https://github.com/octo/two.git" {
		t.Fatalf("unexpected repos: %v", repos)
	}
}
