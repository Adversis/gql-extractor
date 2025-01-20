package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"net/http"
	"regexp"
	"strings"
	"github.com/mafredri/cdp"
	"github.com/mafredri/cdp/rpcc"
	"github.com/tebeka/selenium"
)

// DevToolsResponse is used to parse the response from the Chrome DevTools protocol.
type DevToolsResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerURL"`
}

// Setup Selenium WebDriver using the locally running ChromeDriver and DevTools Protocol
func setupSelenium() (selenium.WebDriver, func(), *cdp.Client, error) {
	const seleniumPath = "http://localhost:4444"

	// Configure ChromeOptions directly in capabilities
	caps := selenium.Capabilities{
		"browserName": "chrome",
		"goog:chromeOptions": map[string]interface{}{
			"args": []string{
				//"--headless", 
				"--disable-gpu", 
				"--no-sandbox", 
				"--remote-debugging-port=9222",
			},
		},
	}

	// Connect to the Selenium WebDriver
	wd, err := selenium.NewRemote(caps, seleniumPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to open session: %v", err)
	}
	log.Println("Selenium session started.")

	// Fetch the WebSocket URL from the DevTools endpoint
	resp, err := http.Get("http://localhost:9222/json")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get DevTools WebSocket URL: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Parse the JSON response
	var devTools []DevToolsResponse
	err = json.Unmarshal(body, &devTools)
	if err != nil || len(devTools) == 0 {
		return nil, nil, nil, fmt.Errorf("failed to parse DevTools response: %v", err)
	}

	// Connect to Chrome DevTools Protocol (CDP)
	conn, err := rpcc.DialContext(context.Background(), devTools[0].WebSocketDebuggerURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to connect to Chrome DevTools: %v", err)
	}
	log.Println("Connected to Chrome DevTools Protocol.")

	client := cdp.NewClient(conn)
	return wd, func() {
		log.Println("Closing Selenium session and Chrome DevTools connection.")
		wd.Quit()
		conn.Close()
	}, client, nil
}

// Capture all network requests to identify JavaScript files
func captureNetworkTraffic(client *cdp.Client, jsURLs chan string) error {
	ctx := context.Background()

	// Enable network events
	if err := client.Network.Enable(ctx, nil); err != nil {
		return fmt.Errorf("failed to enable network tracking: %v", err)
	}

	// Create a subscription to the ResponseReceived events
	responseStream, err := client.Network.ResponseReceived(ctx)
	if err != nil {
		return fmt.Errorf("failed to subscribe to network responses: %v", err)
	}

	log.Println("Started capturing network traffic.")

	// Process network events in a separate goroutine
	go func() {
		defer close(jsURLs)
		for {
			// Read the next response event
			responseEvent, err := responseStream.Recv()
			if err != nil {
				log.Printf("Error receiving network response: %v", err)
				break
			}

			// If the response URL ends with ".js", capture it
			if strings.HasSuffix(responseEvent.Response.URL, ".js") {
				log.Printf("JavaScript file detected: %s", responseEvent.Response.URL)
				jsURLs <- responseEvent.Response.URL
			}
		}
	}()

	return nil
}

// Download and save JavaScript content
func downloadJS(jsURL string) (string, error) {
	log.Printf("Downloading JavaScript file: %s", jsURL)
	resp, err := http.Get(jsURL)
	if err != nil {
		return "", fmt.Errorf("failed to download JS: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read JS content: %v", err)
	}

	return string(body), nil
}

// Extract GQL queries and mutations from JS content using refined regex
func extractGraphQL(content string) ([]string, error) {
	//print logging meddage
	log.Println("Extracting GraphQL queries and mutations from JS content.")
	// Define refined regex patterns to extract queries and mutations
	queryPattern := regexp.MustCompile(`(?s)query\s+[a-zA-Z0-9_]+\s*\([^\)]*\)\s*\{[^}]+\}`)
	mutationPattern := regexp.MustCompile(`(?s)mutation\s+[a-zA-Z0-9_]+\s*\([^\)]*\)\s*\{[^}]+\}`)

	// Find all queries and mutations
	queries := queryPattern.FindAllString(content, -1)
	mutations := mutationPattern.FindAllString(content, -1)

	// Combine both results into a single slice
	return append(queries, mutations...), nil
}

// Save extracted GQL queries and mutations to a file, appending content instead of overwriting
func saveToFile(content []string, fileName string) error {
	if len(content) == 0 {
		log.Println("No GraphQL queries or mutations found.")
		return nil
	}

	// Open the file in append mode, create if not exists
	f, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for appending: %v", err)
	}
	defer f.Close()

	// Join the content and write to the file
	_, err = f.WriteString(strings.Join(content, "\n\n") + "\n\n")
	if err != nil {
		return fmt.Errorf("failed to write to file: %v", err)
	}

	log.Printf("Appended %d GraphQL queries/mutations to file: %s", len(content), fileName)
	return nil
}

func sanitizeDomain(domain string) string {
	// Remove special characters that could cause issues in filenames
	return strings.ReplaceAll(strings.ReplaceAll(domain, "https://", ""), "/", "_")
}

func main() {
	// Command-line flag for the target domain
	domain := flag.String("domain", "", "Target domain to extract GraphQL queries from")
	flag.Parse()

	if *domain == "" {
		log.Fatalf("No domain provided. Please specify a target domain using --domain.")
	}

	// Setup Selenium WebDriver and CDP connection
	wd, cleanup, client, err := setupSelenium()
	if err != nil {
		log.Fatalf("Error setting up Selenium: %v", err)
	}
	defer cleanup()

	// Start capturing JavaScript URLs through network traffic
	jsURLs := make(chan string)
	err = captureNetworkTraffic(client, jsURLs)
	if err != nil {
		log.Fatalf("Error capturing network traffic: %v", err)
	}

	// Load the target page
	log.Printf("Navigating to: %s", *domain)
	err = wd.Get(*domain)
	if err != nil {
		log.Fatalf("Error loading the page: %v", err)
	}

	// Create a dynamic output filename based on the domain name
	sanitizedDomain := sanitizeDomain(*domain)
	outputFileName := fmt.Sprintf("graphql_queries_mutations_%s.graphql", sanitizedDomain)

	// Process JavaScript URLs
	log.Println("Processing JavaScript URLs.")
	for jsURL := range jsURLs {
		// Download JS content
		jsContent, err := downloadJS(jsURL)
		if err != nil {
			log.Printf("Error downloading JS from %s: %v", jsURL, err)
			continue
		}

		// Extract GraphQL queries and mutations
		extractedGQL, err := extractGraphQL(jsContent)
		if err != nil {
			log.Printf("Error extracting GQL from %s: %v", jsURL, err)
			continue
		}

		// Save the extracted GQL queries and mutations to a file
		if len(extractedGQL) > 0 {
			err = saveToFile(extractedGQL, outputFileName)
			if err != nil {
				log.Printf("Error saving extracted GQL to file: %v", err)
			}
		}
	}

	log.Println("Finished processing all JavaScript URLs.")
}
