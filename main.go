package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/gin-gonic/gin"
	"github.com/google/go-github/github"
	"github.com/joho/godotenv"
	logging "github.com/op/go-logging"
)

const (
	GitHubAppID          = 46115
	GitHubInstallationID = 5060780
)

var (
	projectPath = path.Join(os.Getenv("GOPATH"), "src", "github.com", "Zamiell", "hanabi-conventions-issues")
	log         *logging.Logger
	GHClient    *github.Client
)

func main() {
	// Initialize logging
	// http://godoc.org/github.com/op/go-logging#Formatter
	log = logging.MustGetLogger("hanabi-conventions-issues")
	loggingBackend := logging.NewLogBackend(os.Stdout, "", 0)
	logFormat := logging.MustStringFormatter( // https://golang.org/pkg/time/#Time.Format
		`%{time:Mon Jan 02 15:04:05 MST 2006} - %{level:.4s} - %{shortfile} - %{message}`,
	)
	loggingBackendFormatted := logging.NewBackendFormatter(loggingBackend, logFormat)
	logging.SetBackend(loggingBackendFormatted)

	// Welcome message
	log.Info("+-----------------------------------------+")
	log.Info("| Starting hanabi-conventions-issues bot. |")
	log.Info("+-----------------------------------------+")

	// Check to see if the project path exists
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		log.Fatal("The project path of \"" + projectPath + "\" does not exist. " +
			"Check to see if your GOPATH environment variable is set properly.")
		return
	}

	// Load the ".env" file which contains environment variables with secret values
	if err := godotenv.Load(path.Join(projectPath, ".env")); err != nil {
		log.Fatal("Failed to load the \".env\" file:", err)
		return
	}

	// Read some configuration values from environment variables
	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	if len(webhookSecret) == 0 {
		log.Fatal("The \"WEBHOOK_SECRET\" environment variable is blank; set one in your \".env\" file.")
		return
	}

	// Wrap the shared transport for use with our GitHub app
	// https://github.com/bradleyfalzon/ghinstallation
	keyPath := path.Join(projectPath, "key.pem")
	var itr *ghinstallation.Transport
	if v, err := ghinstallation.NewKeyFromFile(http.DefaultTransport, GitHubAppID, GitHubInstallationID, keyPath); err != nil {
		log.Fatal("Failed to read the private key:", err)
	} else {
		itr = v
	}

	// Initialize a GitHub API client
	// https://github.com/google/go-github
	GHClient = github.NewClient(&http.Client{Transport: itr})

	// Create a new Gin HTTP router
	// gin.SetMode(gin.ReleaseMode) // Comment this out to debug HTTP stuff
	httpRouter := gin.New()
	httpRouter.Use(gin.Recovery())
	httpRouter.Use(gin.Logger()) // Uncomment this to enable HTTP request logging
	httpRouter.POST("/", httpPost)

	// Listen and serve (HTTP)
	if err := http.ListenAndServe(
		":8080", // Nothing before the colon implies 0.0.0.0
		httpRouter,
	); err != nil {
		log.Fatal("http.ListenAndServe failed:", err)
		return
	}
	log.Fatal("http.ListenAndServe ended prematurely.")
}

func httpPost(c *gin.Context) {
	// Local variables
	r := c.Request

	// Print out the entire POST request
	/*
		if requestDump, err := httputil.DumpRequest(r, true); err != nil {
			log.Error("Failed to dump the request:", err)
		} else {
			log.Info(string(requestDump))
		}
	*/

	// Data comes to us from the GitHub hook in the form of a JSON POST, so we first decode it
	var event *github.IssueCommentEvent
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&event); err != nil {
		log.Error("Failed to unmarshal the JSON POST:", err)
		return
	}

	// Only respond to messages from Zamiell
	if *event.Sender.Login != "Zamiell" {
		return
	}

	// Look for commands
	msg := "* Some time has passed since this issue was opened and the group appears to have reached a consensus.\n"
	if strings.Contains(*event.Comment.Body, "/deny") {
		msg += "* ❌ This change will **not** be integrated into the official document.\n"
	} else if strings.Contains(*event.Comment.Body, "/accept") {
		msg += "* ✔️ This change will be integrated into the official document.\n"
	} else {
		return
	}

	msg += "* This issue will now be closed; feel free to continue to comment on the issue if you feel that the discussion was ended prematurely.\n"
	msg += "\n(For more information on how consensus is determined, please read the [Convention Changes document](https://github.com/Zamiell/hanabi-conventions/blob/master/misc/Convention_Changes.md).)"

	// Submit the comment
	if _, _, err := GHClient.Issues.CreateComment(
		context.Background(),
		*event.Repo.Owner.Login,
		*event.Repo.Name,
		*event.Issue.Number,
		&github.IssueComment{
			Body: &msg,
		},
	); err != nil {
		log.Error("Failed to create a comment:", err)
	}

	// Close the issue
	closed := "closed"
	if _, _, err := GHClient.Issues.Edit(
		context.Background(),
		*event.Repo.Owner.Login,
		*event.Repo.Name,
		*event.Issue.Number,
		&github.IssueRequest{
			State: &closed,
		},
	); err != nil {
		log.Error("Failed to close the issue:", err)
	}
}
