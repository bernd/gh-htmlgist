package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	_ "text/tabwriter"
	"time"

	ghapi "github.com/cli/go-gh/v2/pkg/api"
	"github.com/kroepke/gh-htmlgist/internal/gist"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "setup":
		err = runSetup()
	case "create":
		err = runCreate(os.Args[2:])
	case "update":
		err = runUpdate(os.Args[2:])
	case "list":
		err = runList()
	case "delete":
		err = runDelete(os.Args[2:])
	case "open":
		err = runOpen(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: gh htmlgist <command> [arguments]

Commands:
  setup              Configure the proxy URL
  create <file...>   Create a new gist from files
  update <id> <file...>  Update an existing gist
  list               List your htmlgist gists
  delete <id>        Delete a gist
  open <id>          Open a gist in the browser
`)
}

func getProxyURL() string {
	out, err := exec.Command("gh", "config", "get", "extensions.htmlgist.proxy-url").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func setProxyURL(url string) error {
	return exec.Command("gh", "config", "set", "extensions.htmlgist.proxy-url", url).Run()
}

func ensureSetup() error {
	if getProxyURL() != "" {
		return nil
	}
	fmt.Println("No proxy URL configured. Running setup...")
	return runSetup()
}

func runSetup() error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Print("Enter your htmlgist proxy URL (e.g. https://gist.internal.example.com): ")
	if !scanner.Scan() {
		return fmt.Errorf("no input received")
	}
	url := strings.TrimSpace(scanner.Text())
	url = strings.TrimRight(url, "/")

	if url == "" {
		return fmt.Errorf("proxy URL cannot be empty")
	}

	fmt.Printf("Checking health endpoint at %s/health...\n", url)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url + "/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not reach proxy at %s/health: %v\n", url, err)
		fmt.Print("Save this URL anyway? [y/N]: ")
		if !scanner.Scan() || strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			return fmt.Errorf("setup cancelled")
		}
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Warning: health check returned status %d\n", resp.StatusCode)
			fmt.Print("Save this URL anyway? [y/N]: ")
			if !scanner.Scan() || strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
				return fmt.Errorf("setup cancelled")
			}
		} else {
			fmt.Println("Health check passed!")
		}
	}

	if err := setProxyURL(url); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Proxy URL saved: %s\n", url)
	return nil
}

func newGistClient() (*gist.Client, error) {
	httpClient, err := ghapi.NewHTTPClient(ghapi.ClientOptions{})
	if err != nil {
		return nil, fmt.Errorf("creating GitHub client: %w", err)
	}
	return gist.NewClient(httpClient), nil
}

func printGistInfo(g gist.Gist) {
	proxyURL := getProxyURL()
	fmt.Printf("Gist ID:    %s\n", g.ID)
	fmt.Printf("GitHub URL: %s\n", g.HTMLURL)
	if proxyURL != "" {
		fmt.Printf("Proxy URL:  %s/%s/\n", proxyURL, g.ID)
	}
}

// Placeholder — implemented in Task 8
func runCreate(args []string) error {
	return fmt.Errorf("not yet implemented")
}

// Placeholder — implemented in Task 9
func runUpdate(args []string) error {
	return fmt.Errorf("not yet implemented")
}

// Placeholder — implemented in Task 9
func runList() error {
	return fmt.Errorf("not yet implemented")
}

// Placeholder — implemented in Task 9
func runDelete(args []string) error {
	return fmt.Errorf("not yet implemented")
}

// Placeholder — implemented in Task 8
func runOpen(args []string) error {
	return fmt.Errorf("not yet implemented")
}
