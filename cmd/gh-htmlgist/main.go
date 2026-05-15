package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	ghapi "github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/browser"
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

func runCreate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gh htmlgist create <file...>")
	}

	if err := ensureSetup(); err != nil {
		return err
	}

	files := make(map[string][]byte, len(args))
	for _, path := range args {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		name := filepath.Base(path)
		files[name] = content
	}

	client, err := newGistClient()
	if err != nil {
		return err
	}

	desc := ""
	for name := range files {
		if strings.HasSuffix(strings.ToLower(name), ".html") || strings.HasSuffix(strings.ToLower(name), ".htm") {
			desc = name
			break
		}
	}
	if desc == "" {
		for name := range files {
			desc = name
			break
		}
	}

	g, err := client.Create(files, desc)
	if err != nil {
		return fmt.Errorf("creating gist: %w", err)
	}

	printGistInfo(g)
	return nil
}

func runList() error {
	client, err := newGistClient()
	if err != nil {
		return err
	}

	gists, err := client.List()
	if err != nil {
		return fmt.Errorf("listing gists: %w", err)
	}

	if len(gists) == 0 {
		fmt.Println("No htmlgist gists found.")
		return nil
	}

	proxyURL := getProxyURL()
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if proxyURL != "" {
		fmt.Fprintf(tw, "ID\tDESCRIPTION\tFILES\tUPDATED\tURL\n")
	} else {
		fmt.Fprintf(tw, "ID\tDESCRIPTION\tFILES\tUPDATED\n")
	}
	for _, g := range gists {
		desc := strings.TrimPrefix(g.Description, gist.DescriptionPrefix)
		fileNames := make([]string, 0, len(g.Files))
		for name := range g.Files {
			fileNames = append(fileNames, name)
		}
		updated := g.UpdatedAt.Format("2006-01-02 15:04")
		if proxyURL != "" {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s/%s/\n", g.ID, desc, strings.Join(fileNames, ", "), updated, proxyURL, g.ID)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", g.ID, desc, strings.Join(fileNames, ", "), updated)
		}
	}
	tw.Flush()

	return nil
}

func runUpdate(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gh htmlgist update <gist-id> <file...>")
	}

	gistID := args[0]
	filePaths := args[1:]

	files := make(map[string][]byte, len(filePaths))
	for _, path := range filePaths {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		name := filepath.Base(path)
		files[name] = content
	}

	client, err := newGistClient()
	if err != nil {
		return err
	}

	g, err := client.Update(gistID, files)
	if err != nil {
		return fmt.Errorf("updating gist: %w", err)
	}

	fmt.Println("Gist updated.")
	printGistInfo(g)
	return nil
}

func runDelete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: gh htmlgist delete <gist-id>")
	}

	client, err := newGistClient()
	if err != nil {
		return err
	}

	if err := client.Delete(args[0]); err != nil {
		return fmt.Errorf("deleting gist: %w", err)
	}

	fmt.Printf("Gist %s deleted.\n", args[0])
	return nil
}

func runOpen(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: gh htmlgist open <gist-id>")
	}

	proxyURL := getProxyURL()
	if proxyURL == "" {
		return fmt.Errorf("no proxy URL configured — run 'gh htmlgist setup' first")
	}

	url := proxyURL + "/" + args[0] + "/"

	b := browser.New("", os.Stdout, os.Stderr)
	return b.Browse(url)
}
