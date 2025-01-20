package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mafredri/cdp"
	"github.com/mafredri/cdp/devtool"
	"github.com/mafredri/cdp/protocol/network"
	"github.com/mafredri/cdp/rpcc"
	"github.com/tebeka/selenium"
)

// DevToolsResponse is used to parse the response from the Chrome DevTools protocol
type DevToolsResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerURL"`
}

// GraphQLCapture represents a captured GraphQL request/response pair
type GraphQLCapture struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
	Response  interface{}            `json:"response,omitempty"`
	Timestamp time.Time             `json:"timestamp"`
	URL       string                `json:"url"`
}

// Setup Selenium WebDriver using the locally running ChromeDriver and DevTools Protocol
func setupSelenium() (selenium.WebDriver, func(), *cdp.Client, error) {
	const seleniumPath = "http://localhost:4444"

	// Configure ChromeOptions directly in capabilities
	caps := selenium.Capabilities{
		"browserName": "chrome",
		"goog:chromeOptions": map[string]interface{}{
			"args": []string{
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

	// Create a new Chrome DevTools Protocol client
	devt := devtool.New("http://localhost:9222")
	pt, err := devt.Get(context.Background(), devtool.Page)
	if err != nil {
		pt, err = devt.Create(context.Background())
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// Connect to Chrome DevTools Protocol
	conn, err := rpcc.DialContext(context.Background(), pt.WebSocketDebuggerURL)
	if err != nil {
		return nil, nil, nil, err
	}

	client := cdp.NewClient(conn)
	return wd, func() {
		log.Println("Closing Selenium session and Chrome DevTools connection.")
		wd.Quit()
		conn.Close()
	}, client, nil
}

// Capture all network requests to identify JavaScript files and GraphQL requests
func captureNetworkTraffic(client *cdp.Client, jsURLs chan string, gqlCaptures chan GraphQLCapture) error {
	ctx := context.Background()

	// Enable network events
	if err := client.Network.Enable(ctx, nil); err != nil {
		return fmt.Errorf("failed to enable network tracking: %v", err)
	}

	// Create subscriptions for network events
	responseStream, err := client.Network.ResponseReceived(ctx)
	if err != nil {
		return fmt.Errorf("failed to subscribe to network responses: %v", err)
	}

	requestStream, err := client.Network.RequestWillBeSent(ctx)
	if err != nil {
		return fmt.Errorf("failed to subscribe to network requests: %v", err)
	}

	log.Println("Started capturing network traffic.")

	// Process network events in a separate goroutine
	go func() {
		defer close(jsURLs)
		defer close(gqlCaptures)

		// Map to store request data temporarily
		requests := make(map[network.RequestID]*network.Request)

		for {
			select {
			case req, ok := <-requestStream.Ready():
				if !ok {
					return
				}
				
				// Store request data
				requests[req.RequestID] = req.Request

				// Check if it's a potential GraphQL request
				if isGraphQLRequest(req.Request) {
					capture := GraphQLCapture{
						Query:     extractQueryFromRequest(req.Request),
						Variables: extractVariablesFromRequest(req.Request),
						Timestamp: time.Now(),
						URL:       req.Request.URL,
					}
					
					if capture.Query != "" {
						gqlCaptures <- capture
					}
				}

			case resp, ok := <-responseStream.Ready():
				if !ok {
					return
				}

				// Handle JavaScript files
				if strings.HasSuffix(resp.Response.URL, ".js") {
					jsURLs <- resp.Response.URL
				}

				// Handle GraphQL responses
				req, exists := requests[resp.RequestID]
				if exists && isGraphQLRequest(req) {
					responseBody, err := client.Network.GetResponseBody(ctx, &network.GetResponseBodyArgs{
						RequestID: resp.RequestID,
					})
					if err == nil && responseBody.Body != "" {
						var responseData interface{}
						if err := json.Unmarshal([]byte(responseBody.Body), &responseData); err == nil {
							capture := GraphQLCapture{
								Query:     extractQueryFromRequest(req),
								Variables: extractVariablesFromRequest(req),
								Response:  responseData,
								Timestamp: time.Now(),
								URL:       resp.Response.URL,
							}
							
							if capture.Query != "" {
								gqlCaptures <- capture
							}
						}
					}
				}

				// Cleanup request data
				delete(requests, resp.RequestID)
			}
		}
	}()

	return nil
}

// Helper functions for GraphQL request handling
func isGraphQLRequest(req *network.Request) bool {
	// Check URL path
	if strings.Contains(strings.ToLower(req.URL), "graphql") {
		return true
	}

	// Check Content-Type header
	if strings.Contains(strings.ToLower(req.Headers["Content-Type"]), "application/graphql") {
		return true
	}

	// Check request body for GraphQL keywords
	if req.PostData != nil {
		return strings.Contains(*req.PostData, "query") || strings.Contains(*req.PostData, "mutation")
	}

	return false
}

func extractQueryFromRequest(req *network.Request) string {
	if req.PostData == nil {
		return ""
	}

	var requestData struct {
		Query string `json:"query"`
	}

	if err := json.Unmarshal([]byte(*req.PostData), &requestData); err != nil {
		return ""
	}

	return requestData.Query
}

func extractVariablesFromRequest(req *network.Request) map[string]interface{} {
	if req.PostData == nil {
		return nil
	}

	var requestData struct {
		Variables map[string]interface{} `json:"variables"`
	}

	if err := json.Unmarshal([]byte(*req.PostData), &requestData); err != nil {
		return nil
	}

	return requestData.Variables
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
	log.Println("Extracting GraphQL queries and mutations from JS content.")
	queryPattern := regexp.MustCompile(`(?s)query\s+[a-zA-Z0-9_]+\s*\([^\)]*\)\s*\{[^}]+\}`)
	mutationPattern := regexp.MustCompile(`(?s)mutation\s+[a-zA-Z0-9_]+\s*\([^\)]*\)\s*\{[^}]+\}`)

	queries := queryPattern.FindAllString(content, -1)
	mutations := mutationPattern.FindAllString(content, -1)

	return append(queries, mutations...), nil
}

// Format GraphQL query with proper indentation and spacing
func formatGraphQLQuery(query string) string {
	query = strings.TrimSpace(query)
	indent := 0
	var formatted strings.Builder

	lines := strings.Split(query, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "}") {
			indent--
		}

		formatted.WriteString(strings.Repeat("  ", indent))
		formatted.WriteString(line)
		formatted.WriteString("\n")

		if strings.HasSuffix(line, "{") {
			indent++
		}

		if strings.HasSuffix(line, "}") && !strings.HasPrefix(line, "}") {
			indent--
		}
	}

	return formatted.String()
}

// Save extracted GQL queries and mutations to a file
func saveToFile(content []string, captures []GraphQLCapture, fileName string) error {
	f, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for appending: %v", err)
	}
	defer f.Close()

	// Write static queries found in JS files
	for _, query := range content {
		formattedQuery := formatGraphQLQuery(query)
		_, err = f.WriteString(fmt.Sprintf("\n# Static Query found at: %s\n", time.Now().Format(time.RFC3339)))
		if err != nil {
			return fmt.Errorf("failed to write static query: %v", err)
		}
		
		_, err = f.WriteString(formattedQuery + "\n\n# " + strings.Repeat("-", 50) + "\n\n")
		if err != nil {
			return fmt.Errorf("failed to write formatted query: %v", err)
		}
	}

	// Write captured network queries
	for _, capture := range captures {
		_, err = f.WriteString(fmt.Sprintf("\n# GraphQL Network Capture at: %s\n", capture.Timestamp.Format(time.RFC3339)))
		if err != nil {
			return fmt.Errorf("failed to write capture timestamp: %v", err)
		}

		_, err = f.WriteString(fmt.Sprintf("# URL: %s\n", capture.URL))
		if err != nil {
			return fmt.Errorf("failed to write capture URL: %v", err)
		}

		formattedQuery := formatGraphQLQuery(capture.Query)
		_, err = f.WriteString(fmt.Sprintf("\n# Query:\n%s\n", formattedQuery))
		if err != nil {
			return fmt.Errorf("failed to write capture query: %v", err)
		}

		if len(capture.Variables) > 0 {
			variablesJSON, _ := json.MarshalIndent(capture.Variables, "", "  ")
			_, err = f.WriteString(fmt.Sprintf("\n# Variables:\n%s\n", string(variablesJSON)))
			if err != nil {
				return fmt.Errorf("failed to write capture variables: %v", err)
			}
		}

		if capture.Response != nil {
			responseJSON, _ := json.MarshalIndent(capture.Response, "", "  ")
			_, err = f.WriteString(fmt.Sprintf("\n# Response:\n%s\n", string(responseJSON)))
			if err != nil {
				return fmt.Errorf("failed to write capture response: %v", err)
			}
		}

		_, err = f.WriteString("\n# " + strings.Repeat("-", 50) + "\n\n")
		if err != nil {
			return fmt.Errorf("failed to write separator: %v", err)
		}
	}

	return nil
}

func sanitizeDomain(domain string) string {
	return strings.ReplaceAll(strings.ReplaceAll(domain, "https://", ""), "/", "_")
}

func main() {
	domain := flag.String("domain", "", "Target domain to extract GraphQL queries from")
	flag.Parse()

	if *domain == "" {
		log.Fatalf("No domain provided. Please specify a target domain using --domain.")
	}

	wd, cleanup, client, err := setupSelenium()
	if err != nil {
		log.Fatalf("Error setting up Selenium: %v", err)
	}
	defer cleanup()

	jsURLs := make(chan string)
	gqlCaptures := make(chan GraphQLCapture)
	var captures []GraphQLCapture

	err = captureNetworkTraffic(client, jsURLs, gqlCaptures)
	if err != nil {
		log.Fatalf("Error capturing network traffic: %v", err)
	}

	// Start a goroutine to collect captures
	go func() {
		for capture := range gqlCaptures {
			captures = append(captures, capture)
		}
	}()

	log.Printf("Navigating to: %s", *domain)
	err = wd.Get(*domain)
	if err != nil {
		log.Fatalf("Error loading the page: %v", err)
	}

	sanitizedDomain := sanitizeDomain(*domain)
	outputFileName := fmt.Sprintf("graphql_queries_mutations_%s.graphql", sanitizedDomain)

	var extractedQueries []string

	log.Println("Processing JavaScript URLs.")
	for jsURL := range jsURLs {
		jsContent, err := downloadJS(jsURL)
		if err != nil {
			log.Printf("Error downloading JS from %s: %v", jsURL, err)
			continue
		}

		queries, err := extractGraphQL(jsContent)
		if err != nil {
			log.Printf("Error extracting GQL from %s: %v", jsURL, err)
			continue
		}

		extractedQueries = append(extractedQueries, queries...)
	}

	if err := saveToFile(extractedQueries, captures, outputFileName); err != nil {
		log.Printf("Error saving to file: %v", err)
	}

	log.Println("Finished processing all JavaScript URLs and network captures.")
}
