/*
Sniperkit-Bot
- Status: analyzed
*/

// Package main implements a githelper command to create a GitHub and/or Gogs
// repository and optionally a Jenkins job as well.
// Available flags:
// * github: A boolean flag to create a GitHub repository for this project (defaults to false)
// * gogs: A boolean flag to create a Gogs repository for this project (defaults to false)
// * jenkins: A boolean flag to create a Jenkins DSL for this project (defaults to false)
// * commit: A boolean flag to commit and push the updates to the Jenkins DSL project (defaults to false)
// * github-token: The token to use to connect to GitHub (optional)
// * gogs-token: The token to use to connect to Gogs (optional)
// * jenkins-base: The base directory of the Jenkins DSL project (optional)
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	github      bool
	gogs        bool
	jenkins     bool
	commit      bool
	githubToken string
	gogsToken   string
	jenkinsBase string
)

var jenkinsDSLTemplate = `// Project information
String project = "{{.reponame}}"
String icon = "search.png"

// GitHub information
String gitHubRepository = "{{.reponame}}"
String gitHubUser = "retgits"

// Gogs information
String gogsRepository = "{{.reponame}}"
String gogsUser = "retgits"
String gogsHost = "ubusrvls.na.tibco.com:3000"

// Job DSL definition
freeStyleJob("mirror-$project") {
 displayName("mirror-$project")
 description("Mirror github.com/$gitHubUser/$gitHubRepository")

 checkoutRetryCount(3)

 properties {
  githubProjectUrl("https://github.com/$gitHubUser/$gitHubRepository")
  sidebarLinks {
   link("http://$gogsHost/$gogsUser/$gogsRepository", "Gogs", "$icon")
  }
 }

 logRotator {
  numToKeep(100)
  daysToKeep(15)
 }

 triggers {
  cron('@daily')
 }

 wrappers {
  colorizeOutput()
  credentialsBinding {
   usernamePassword('GOGS_USERPASS', 'gogs')
  }
 }

 steps {
  shell("git clone --mirror https://github.com/$gitHubUser/$gitHubRepository repo")
  shell("cd repo && git push --mirror http://\$GOGS_USERPASS@gogs:3000/$gogsUser/$gogsRepository")
 }

 publishers {
  mailer {
   recipients('$ADMIN_EMAIL')
   notifyEveryUnstableBuild(true)
   sendToIndividuals(false)
  }
  wsCleanup()
 }
}`

func main() {
	// Prepare flags
	flag.BoolVar(&github, "github", false, "Create a GitHub repository for this project")
	flag.BoolVar(&gogs, "gogs", false, "Create a Gogs repository for this project")
	flag.BoolVar(&jenkins, "jenkins", false, "Create a Jenkins DSL for this project")
	flag.BoolVar(&commit, "commit", false, "Commit and push the updates to the Jenkins DSL project")
	flag.StringVar(&githubToken, "github-token", "", "The Personal Access Token for GitHub (optional)")
	flag.StringVar(&gogsToken, "gogs-token", "", "The Personal Access Token for Gogs (optional)")
	flag.StringVar(&jenkinsBase, "jenkins-base", "", "The base directory of the Jenkins DSL project (optional)")
	flag.Parse()

	// Get the current directory
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		fmt.Printf("%s\n", err.Error())
	}

	// Get the base directory
	base := filepath.Base(dir)

	// Get the Gogs token. The precedence is as follows:
	// 1) Flag   : gogs-token
	// 2) Env var: GOGSTOKEN
	if gogs {
		if len(gogsToken) == 0 {
			gogsToken = os.Getenv("GOGSTOKEN")
			if len(gogsToken) == 0 {
				fmt.Println("Cannot find Gogs token from flags or environment")
			}
		}
		createGogsRepository(base, gogsToken)
	}

	// Get the GitHub token. The precedence is as follows:
	// 1) Flag   : github-token
	// 2) Env var: GITHUBTOKEN
	if github {
		if len(githubToken) == 0 {
			githubToken = os.Getenv("GITHUBTOKEN")
			if len(githubToken) == 0 {
				fmt.Println("Cannot find GitHub token from flags or environment")
			}
		}
		createGitHubRepository(base, githubToken)
	}

	// Create a Jenkins job.The precedence is as follows:
	// 1) Flag   : jenkins-base
	// 2) Env var: JENKINSBASEDIR
	if jenkins {
		if len(jenkinsBase) == 0 {
			jenkinsBase = os.Getenv("JENKINSBASEDIR")
			if len(jenkinsBase) == 0 {
				fmt.Println("Cannot find Jenkins base directory from flags or environment")
			}
		}
		createJenkinsJob(base, jenkinsBase, commit)
	}
}

func createJenkinsJob(reponame string, jenkinsBase string, commit bool) {
	// Prepare a map
	data := make(map[string]interface{})
	data["reponame"] = reponame

	// Prepare the template
	t := template.Must(template.New("email").Parse(jenkinsDSLTemplate))
	buf := &bytes.Buffer{}
	if err := t.Execute(buf, data); err != nil {
		fmt.Printf("There was a problem creating the Jenkins template\n%s\n", err.Error())
	}
	s := buf.String()

	// Write the template to disk
	file, err := os.Create(filepath.Join(jenkinsBase, "projects", fmt.Sprintf("%s.groovy", reponame)))
	if err != nil {
		fmt.Printf("There was a problem creating the template file\n%s\n", err.Error())
	}
	defer file.Close()

	_, err = file.WriteString(s)
	if err != nil {
		fmt.Printf("There was a problem writing the template file\n%s\n", err.Error())
	}

	err = file.Sync()
	if err != nil {
		fmt.Printf("There was a problem syncing the template file\n%s\n", err.Error())
	}

	// Push to GitHub
	if commit {
		cmd := exec.Command("git", "add", "-A", ".", "&&", "git", "commit", "-a", "-m", fmt.Sprintf("\"Add new job for %s\"", reponame), "&&", "git", "push", "origin", "master")
		cmd.Dir = jenkinsBase
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			fmt.Printf("There was a problem pushing to GitHub\n%s\n", err.Error())
		}
	}
}

func createGitHubRepository(reponame string, token string) {
	// Prepare the payload
	jsonString := fmt.Sprintf(`{"name":"%s"}`, reponame)

	// Prepare the API request
	req, err := http.NewRequest("POST", "https://api.github.com/user/repos", strings.NewReader(jsonString))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))

	// Prepare the HTTP client
	client := &http.Client{}

	// Execute the HTTP request
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		fmt.Printf("GitHub did not response with HTTP/201\n")
		fmt.Printf("  HTTP StatusCode %v\n", resp.StatusCode)
		fmt.Printf("  HTTP Body %v\n", resp.Body)
	}

	fmt.Println(resp.Body)
}

func createGogsRepository(reponame string, token string) {
	// Prepare the payload
	jsonString := fmt.Sprintf(`{"name":"%s"}`, reponame)

	// Prepare the API request
	req, err := http.NewRequest("POST", "http://ubusrvls.na.tibco.com:3000/api/v1/user/repos", strings.NewReader(jsonString))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))

	fmt.Println(req)

	// Prepare the HTTP client
	client := &http.Client{}

	// Execute the HTTP request
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		fmt.Printf("Gogs did not response with HTTP/201\n")
		fmt.Printf("  HTTP StatusCode %v\n", resp.StatusCode)
		fmt.Printf("  HTTP Body %v\n", resp.Body)
	}
}
