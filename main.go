package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth_gin"
	"github.com/gin-gonic/gin"
	"github.com/google/go-github/github"
	"github.com/joho/godotenv"
	logging "github.com/op/go-logging"
	"github.com/rjz/githubhook"
)

var (
	logger              *logging.Logger
	projectPath         string
	gitHubWebhookSecret string
	ghClient            *github.Client
)

func main() {
	// Initialize logging
	// http://godoc.org/github.com/op/go-logging#Formatter
	logger = logging.MustGetLogger("hanabi-conventions-issues")
	loggingBackend := logging.NewLogBackend(os.Stdout, "", 0)
	logFormat := logging.MustStringFormatter( // https://golang.org/pkg/time/#Time.Format
		`%{time:Mon Jan 02 15:04:05 MST 2006} - %{level:.4s} - %{shortfile} - %{message}`,
	)
	loggingBackendFormatted := logging.NewBackendFormatter(loggingBackend, logFormat)
	logging.SetBackend(loggingBackendFormatted)

	// Welcome message
	logger.Info("+-----------------------------------------+")
	logger.Info("| Starting hanabi-conventions-issues bot. |")
	logger.Info("+-----------------------------------------+")

	// Get the project path
	// https://stackoverflow.com/questions/18537257/
	if v, err := os.Executable(); err != nil {
		logger.Fatal("Failed to get the path of the currently running executable:", err)
	} else {
		projectPath = filepath.Dir(v)
	}

	// Load the ".env" file which contains environment variables with secret values
	if err := godotenv.Load(path.Join(projectPath, ".env")); err != nil {
		log.Fatal("Failed to load the \".env\" file:", err)
		return
	}

	// Read some configuration values from environment variables
	gitHubAppIDString := os.Getenv("GITHUB_APP_ID")
	if len(gitHubAppIDString) == 0 {
		log.Fatal("The \"GITHUB_APP_ID\" environment variable is blank; set one in your \".env\" file.")
		return
	}
	var gitHubAppID int64
	if v, err := strconv.ParseInt(gitHubAppIDString, 10, 64); err != nil {
		log.Fatal("The  \"GITHUB_APP_ID\" environment variable of \"" + gitHubAppIDString + "\" is not a number.")
		return
	} else {
		gitHubAppID = v
	}
	gitHubInstallationIDString := os.Getenv("GITHUB_INSTALLATION_ID")
	if len(gitHubInstallationIDString) == 0 {
		log.Fatal("The \"GITHUB_INSTALLATION_ID\" environment variable is blank; set one in your \".env\" file.")
		return
	}
	var gitHubInstallationID int64
	if v, err := strconv.ParseInt(gitHubInstallationIDString, 10, 64); err != nil {
		log.Fatal("The  \"GITHUB_INSTALLATION_ID\" environment variable of \"" + gitHubInstallationIDString + "\" is not a number.")
		return
	} else {
		gitHubInstallationID = v
	}
	gitHubWebhookSecret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	if len(gitHubWebhookSecret) == 0 {
		log.Fatal("The \"GITHUB_WEBHOOK_SECRET\" environment variable is blank; set one in your \".env\" file.")
		return
	}

	// Wrap the shared transport for use with our GitHub app
	// https://github.com/bradleyfalzon/ghinstallation
	// (see the ".env" file for instructions on getting these values)
	gitHubKeyPath := path.Join(projectPath, "GitHub_private_key.pem")
	var transport *ghinstallation.Transport
	if v, err := ghinstallation.NewKeyFromFile(
		http.DefaultTransport,
		gitHubAppID,
		gitHubInstallationID,
		gitHubKeyPath,
	); err != nil {
		log.Fatal("Failed to read the private key from \""+gitHubKeyPath+"\":", err)
	} else {
		transport = v
	}

	// Initialize a GitHub API client
	// https://github.com/google/go-github
	ghClient = github.NewClient(&http.Client{
		Transport: transport,
	})

	// Create a new Gin HTTP router
	gin.SetMode(gin.ReleaseMode)
	httpRouter := gin.Default() // Has the "Logger" and "Recovery" middleware attached

	// Attach rate-limiting middleware from Tollbooth
	limiter := tollbooth.NewLimiter(1, nil) // Limit each user to 1 requests per second
	limiter.SetMessage(http.StatusText(http.StatusTooManyRequests))
	limiterMiddleware := tollbooth_gin.LimitHandler(limiter)
	httpRouter.Use(limiterMiddleware)

	// Attach path handlers
	httpRouter.POST("/", httpPost)

	// Listen and serve (HTTP)
	if err := http.ListenAndServe(
		":8080", // Nothing before the colon implies 0.0.0.0
		httpRouter,
	); err != nil {
		logger.Fatal("http.ListenAndServe failed:", err)
		return
	}
	logger.Fatal("http.ListenAndServe ended prematurely.")
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

	// Use the githubhook library to verify that this message was sent from GitHub
	// (with the configured webhook secret)
	var hook *githubhook.Hook
	if v, err := githubhook.Parse([]byte(gitHubWebhookSecret), r); err != nil {
		logger.Error("Failed to validate the webhook secret:", err)
		return
	} else {
		hook = v
	}

	// Data comes to us from the GitHub hook in the form of a JSON POST, so we first decode it
	event := github.IssueCommentEvent{}
	if err := json.Unmarshal(hook.Payload, &event); err != nil {
		logger.Error("Failed to unmarshal the JSON POST:", err)
		return
	}

	// Only respond to messages from Zamiell
	if *event.Sender.Login != "Zamiell" {
		return
	}

	// Look for commands
	msg := ""
	if strings.Contains(*event.Comment.Body, "/deny") ||
		strings.Contains(*event.Comment.Body, "/reject") {

		msg += "* Some time has passed since this issue was opened and the group appears to have reached a consensus.\n"
		msg += "* ❌ This change will **not** be integrated into the official reference document.\n"

	} else if strings.Contains(*event.Comment.Body, "/accept") {
		msg += "* Some time has passed since this issue was opened and the group appears to have reached a consensus.\n"
		msg += "* ✔️ This change will be integrated into the official reference document.\n"

	} else if strings.Contains(*event.Comment.Body, "/stale") ||
		strings.Contains(*event.Comment.Body, "/idle") ||
		strings.Contains(*event.Comment.Body, "/zzz") {

		msg += "* Some time has passed since this issue was opened and the discussion appears to have died down.\n"
		msg += "* 💤 Either the document has already been updated or no additional changes need to be made.\n"
	} else {
		return
	}

	msg += "* This issue will now be closed. If you feel this was an error, feel free to continue the discussion and a moderator will re-open the issue.\n"
	msg += "\n(For more information on how consensus is determined, please read the [Convention Changes document](https://github.com/hanabi/hanabi.github.io/blob/main/misc/convention-changes.md).)"

	// Submit the comment
	if _, _, err := ghClient.Issues.CreateComment(
		context.Background(),
		*event.Repo.Owner.Login,
		*event.Repo.Name,
		*event.Issue.Number,
		&github.IssueComment{
			Body: &msg,
		},
	); err != nil {
		logger.Error("Failed to create a comment:", err)
	}

	// Close the issue
	closed := "closed"
	if _, _, err := ghClient.Issues.Edit(
		context.Background(),
		*event.Repo.Owner.Login,
		*event.Repo.Name,
		*event.Issue.Number,
		&github.IssueRequest{
			State: &closed,
		},
	); err != nil {
		logger.Error("Failed to close the issue:", err)
	}
}
