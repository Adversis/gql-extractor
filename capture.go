package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// Progress tracks the progress of the extraction
type Progress struct {
	JSFilesFound      int32
	JSFilesProcessed  int32
	JSFilesDownloaded int32
	TotalBytesDownloaded int64
	QueriesFound      int32
	MutationsFound    int32
	NetworkCaptures   int32
	StartTime         time.Time
	mu                sync.Mutex
	jsFileList        []string
}

func (p *Progress) AddJSFile(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jsFileList = append(p.jsFileList, url)
	atomic.AddInt32(&p.JSFilesFound, 1)
}

func (p *Progress) Report() {
	elapsed := time.Since(p.StartTime)
	found := atomic.LoadInt32(&p.JSFilesFound)
	processed := atomic.LoadInt32(&p.JSFilesProcessed)
	downloaded := atomic.LoadInt32(&p.JSFilesDownloaded)
	bytes := atomic.LoadInt64(&p.TotalBytesDownloaded)
	queries := atomic.LoadInt32(&p.QueriesFound)
	mutations := atomic.LoadInt32(&p.MutationsFound)
	captures := atomic.LoadInt32(&p.NetworkCaptures)
	
	log.Printf("Progress Report [%s elapsed]:", elapsed.Round(time.Second))
	log.Printf("  JS Files: %d found, %d downloaded, %d processed", found, downloaded, processed)
	log.Printf("  Data: %.2f MB downloaded", float64(bytes)/(1024*1024))
	log.Printf("  GraphQL: %d queries, %d mutations found", queries, mutations)
	log.Printf("  Network: %d GraphQL requests captured", captures)
	
	// Show current processing files
	p.mu.Lock()
	if processed < found && int(processed) < len(p.jsFileList) {
		log.Printf("  Currently processing: %s", p.jsFileList[processed])
	}
	p.mu.Unlock()
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
func captureNetworkTraffic(client *cdp.Client, jsURLs chan string, gqlCaptures chan GraphQLCapture, progress *Progress) error {
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
			case <-requestStream.Ready():
				req, err := requestStream.Recv()
				if err != nil {
					return
				}
				
				// Store request data
				requests[req.RequestID] = &req.Request

				// Check if it's a potential GraphQL request
				if isGraphQLRequest(&req.Request) {
					capture := GraphQLCapture{
						Query:     extractQueryFromRequest(&req.Request),
						Variables: extractVariablesFromRequest(&req.Request),
						Timestamp: time.Now(),
						URL:       req.Request.URL,
					}
					
					if capture.Query != "" {
						atomic.AddInt32(&progress.NetworkCaptures, 1)
						gqlCaptures <- capture
					}
				}

			case <-responseStream.Ready():
				resp, err := responseStream.Recv()
				if err != nil {
					return
				}

				// Handle JavaScript files
				if strings.HasSuffix(resp.Response.URL, ".js") {
					progress.AddJSFile(resp.Response.URL)
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
								atomic.AddInt32(&progress.NetworkCaptures, 1)
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
	headers, err := req.Headers.Map()
	if err == nil {
		contentType, exists := headers["Content-Type"]
		if exists && strings.Contains(strings.ToLower(contentType), "application/graphql") {
			return true
		}
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

// Download and save JavaScript content with progress tracking
func downloadJS(jsURL string, progress *Progress) (string, error) {
	log.Printf("Downloading: %s", jsURL)
	
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	
	resp, err := client.Get(jsURL)
	if err != nil {
		return "", fmt.Errorf("failed to download JS: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read JS content: %v", err)
	}

	size := int64(len(body))
	atomic.AddInt64(&progress.TotalBytesDownloaded, size)
	atomic.AddInt32(&progress.JSFilesDownloaded, 1)
	
	log.Printf("Downloaded: %s (%.2f KB)", jsURL, float64(size)/1024)

	return string(body), nil
}

// Extract GQL queries and mutations from JS content using the parser
func extractGraphQL(content string, progress *Progress) ([]*GraphQLOperation, error) {
	log.Println("Extracting GraphQL queries and mutations...")
	
	operations, err := ExtractOperationsFromJS(content)
	if err != nil {
		return nil, err
	}
	
	// Count operations by type
	for _, op := range operations {
		switch op.Type {
		case Query:
			atomic.AddInt32(&progress.QueriesFound, 1)
		case Mutation:
			atomic.AddInt32(&progress.MutationsFound, 1)
		}
	}

	log.Printf("Found %d operations (%d queries, %d mutations)", 
		len(operations), 
		atomic.LoadInt32(&progress.QueriesFound),
		atomic.LoadInt32(&progress.MutationsFound))

	return operations, nil
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

// saveOperations saves GraphQL operations in multiple formats
func saveOperations(operations []*GraphQLOperation, captures []GraphQLCapture, baseName string) error {
	// Create output directory
	outputDir := "output"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}
	
	// Deduplicate operations
	unique := DeduplicateOperations(operations)
	log.Printf("Deduplicated %d operations to %d unique operations", len(operations), len(unique))
	
	// Save in SDL format
	sdlFile := filepath.Join(outputDir, baseName + ".graphql")
	sdlContent := ExportToSDL(unique)
	if err := os.WriteFile(sdlFile, []byte(sdlContent), 0644); err != nil {
		return fmt.Errorf("failed to save SDL file: %v", err)
	}
	log.Printf("Saved SDL format to: %s", sdlFile)
	
	// Save in JSON format
	jsonFile := filepath.Join(outputDir, baseName + ".json")
	jsonContent, err := ExportToJSON(unique, captures)
	if err != nil {
		return fmt.Errorf("failed to generate JSON: %v", err)
	}
	if err := os.WriteFile(jsonFile, jsonContent, 0644); err != nil {
		return fmt.Errorf("failed to save JSON file: %v", err)
	}
	log.Printf("Saved JSON format to: %s", jsonFile)
	
	// Save detailed capture log
	logFile := filepath.Join(outputDir, baseName + "_detailed.log")
	if err := saveDetailedLog(unique, captures, logFile); err != nil {
		return fmt.Errorf("failed to save detailed log: %v", err)
	}
	log.Printf("Saved detailed log to: %s", logFile)
	
	return nil
}

// saveDetailedLog saves a detailed log with all captures and responses
func saveDetailedLog(operations []*GraphQLOperation, captures []GraphQLCapture, fileName string) error {
	f, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer f.Close()
	
	fmt.Fprintf(f, "# GraphQL Operations Detailed Log\n")
	fmt.Fprintf(f, "# Generated at: %s\n\n", time.Now().Format(time.RFC3339))
	
	// Write static operations
	if len(operations) > 0 {
		fmt.Fprintf(f, "## Static Operations Found in JavaScript\n\n")
		for i, op := range operations {
			fmt.Fprintf(f, "### Operation %d: %s %s\n", i+1, op.Type, op.Name)
			if len(op.Variables) > 0 {
				fmt.Fprintf(f, "Variables: %v\n", op.Variables)
			}
			fmt.Fprintf(f, "```graphql\n%s\n```\n\n", op.Raw)
		}
	}
	
	// Write network captures
	if len(captures) > 0 {
		fmt.Fprintf(f, "## Network Captures\n\n")
		for i, capture := range captures {
			fmt.Fprintf(f, "### Capture %d\n", i+1)
			fmt.Fprintf(f, "- Time: %s\n", capture.Timestamp.Format(time.RFC3339))
			fmt.Fprintf(f, "- URL: %s\n\n", capture.URL)
			
			if capture.Query != "" {
				fmt.Fprintf(f, "#### Query\n```graphql\n%s\n```\n\n", capture.Query)
			}
			
			if len(capture.Variables) > 0 {
				varsJSON, _ := json.MarshalIndent(capture.Variables, "", "  ")
				fmt.Fprintf(f, "#### Variables\n```json\n%s\n```\n\n", string(varsJSON))
			}
			
			if capture.Response != nil {
				respJSON, _ := json.MarshalIndent(capture.Response, "", "  ")
				// Truncate very long responses
				if len(respJSON) > 5000 {
					respJSON = append(respJSON[:5000], []byte("\n... [truncated]")...)
				}
				fmt.Fprintf(f, "#### Response\n```json\n%s\n```\n\n", string(respJSON))
			}
			
			fmt.Fprintf(f, "---\n\n")
		}
	}
	
	return nil
}

func sanitizeDomain(domain string) string {
	return strings.ReplaceAll(strings.ReplaceAll(domain, "https://", ""), "/", "_")
}

func main() {
	domain := flag.String("domain", "", "Target domain to extract GraphQL queries from")
	timeout := flag.Duration("timeout", 5*time.Minute, "Maximum time to wait for page to load and process")
	progressInterval := flag.Duration("progress", 10*time.Second, "Progress report interval")
	flag.Parse()

	if *domain == "" {
		log.Fatalf("No domain provided. Please specify a target domain using --domain.")
	}

	// Initialize progress tracking
	progress := &Progress{
		StartTime: time.Now(),
	}

	// Start progress reporting
	progressTicker := time.NewTicker(*progressInterval)
	defer progressTicker.Stop()
	
	go func() {
		for range progressTicker.C {
			progress.Report()
		}
	}()

	// Setup timeout
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	wd, cleanup, client, err := setupSelenium()
	if err != nil {
		log.Fatalf("Error setting up Selenium: %v", err)
	}
	defer cleanup()

	jsURLs := make(chan string, 100) // Buffer to prevent blocking
	gqlCaptures := make(chan GraphQLCapture, 100)
	var captures []GraphQLCapture

	err = captureNetworkTraffic(client, jsURLs, gqlCaptures, progress)
	if err != nil {
		log.Fatalf("Error capturing network traffic: %v", err)
	}

	// Start a goroutine to collect captures
	capturesDone := make(chan struct{})
	go func() {
		for capture := range gqlCaptures {
			captures = append(captures, capture)
		}
		close(capturesDone)
	}()

	log.Printf("Navigating to: %s", *domain)
	err = wd.Get(*domain)
	if err != nil {
		log.Fatalf("Error loading the page: %v", err)
	}

	// Wait a bit for the page to load and make requests
	log.Println("Waiting for page to fully load and make GraphQL requests...")
	select {
	case <-time.After(10 * time.Second):
	case <-ctx.Done():
		log.Println("Timeout reached while waiting for page load")
	}

	sanitizedDomain := sanitizeDomain(*domain)
	baseFileName := fmt.Sprintf("graphql_operations_%s", sanitizedDomain)

	var allOperations []*GraphQLOperation
	processedURLs := make(map[string]bool)

	log.Println("Processing JavaScript files...")
	log.Println("Continue browsing to capture more queries. Close the browser when done.")
	
	// Monitor browser session
	sessionDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		
		for range ticker.C {
			// Check if browser session is still active
			_, err := wd.CurrentURL()
			if err != nil {
				log.Println("Browser session ended")
				close(sessionDone)
				return
			}
		}
	}()
	
	// Process JS files continuously until the browser is closed
	processing := true
	for processing {
		select {
		case jsURL, ok := <-jsURLs:
			if !ok {
				// Channel closed, network monitoring ended
				processing = false
				break
			}
			
			// Skip if already processed
			if processedURLs[jsURL] {
				continue
			}
			processedURLs[jsURL] = true

			jsContent, err := downloadJS(jsURL, progress)
			if err != nil {
				log.Printf("Error downloading JS from %s: %v", jsURL, err)
				continue
			}

			operations, err := extractGraphQL(jsContent, progress)
			if err != nil {
				log.Printf("Error extracting GQL from %s: %v", jsURL, err)
				continue
			}

			allOperations = append(allOperations, operations...)
			atomic.AddInt32(&progress.JSFilesProcessed, 1)
			
		case <-sessionDone:
			log.Println("Browser closed by user, finishing up...")
			processing = false
			
		case <-ctx.Done():
			log.Println("Timeout reached, stopping processing")
			processing = false
		}
	}

	// Final progress report
	progress.Report()

	// Wait for captures to finish
	<-capturesDone

	// Convert network captures to operations
	for _, capture := range captures {
		if capture.Query != "" {
			op, err := ParseGraphQLOperation(capture.Query)
			if err == nil {
				// Add variables from capture
				if len(capture.Variables) > 0 && len(op.Variables) == 0 {
					op.Variables = make(map[string]string)
					for k := range capture.Variables {
						op.Variables[k] = "Any" // Default type
					}
				}
				allOperations = append(allOperations, op)
			}
		}
	}
	
	log.Printf("Saving results...")
	if err := saveOperations(allOperations, captures, baseFileName); err != nil {
		log.Printf("Error saving files: %v", err)
	}

	log.Printf("\nExtraction complete!")
	log.Printf("Total JS files processed: %d", atomic.LoadInt32(&progress.JSFilesProcessed))
	log.Printf("Total data downloaded: %.2f MB", float64(atomic.LoadInt64(&progress.TotalBytesDownloaded))/(1024*1024))
	log.Printf("Total queries found: %d", atomic.LoadInt32(&progress.QueriesFound))
	log.Printf("Total mutations found: %d", atomic.LoadInt32(&progress.MutationsFound))
	log.Printf("Total network captures: %d", atomic.LoadInt32(&progress.NetworkCaptures))
	log.Printf("Total unique operations: %d", len(DeduplicateOperations(allOperations)))
	log.Printf("Results saved to output/ directory with base name: %s", baseFileName)
}