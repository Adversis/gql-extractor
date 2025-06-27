# GraphQL Query Extractor

<p align="center">
  <img width="400" alt="Screenshot 2025-01-20 at 10 51 50 AM" src="https://github.com/user-attachments/assets/bc5031e7-5838-479c-a1bc-759052762683" />
</p>

A tool that extracts GraphQL queries and mutations from web applications by analyzing JavaScript files and monitoring network traffic during browser sessions.

## Features

- Automatic Chrome version detection and ChromeDriver download
- Real-time progress tracking with detailed statistics
- Network traffic monitoring for GraphQL requests
- JavaScript file analysis for embedded queries
- Configurable timeouts and progress intervals
- Support for authenticated sessions through browser interaction
- **Automatic deduplication of GraphQL operations**
- **Multiple output formats: SDL (.graphql), JSON, and detailed logs**
- **Operation signature and variable extraction**
- **Type inference from captured responses**

## Prerequisites

- Go 1.19 or later
- Google Chrome browser
- macOS, Linux, or Windows

## Quick Start

1. Clone the repository:
```bash
git clone https://github.com/yourusername/gql-extractor
cd gql-extractor
```

2. Run the tool:
```bash
make run DOMAIN="https://example.com"
```

The tool will automatically:
- Detect your Chrome version
- Download the matching ChromeDriver
- Start the extraction process
- Show progress updates every 10 seconds
- Continue processing new JavaScript files as you browse
- Save results when you close the browser

## Usage

### Basic Usage

```bash
make run DOMAIN="https://target-domain.com"
```

### Advanced Options

```bash
# Run with custom overall timeout (default: 5 minutes)
./bin/gql-extractor --domain="https://example.com" --timeout=10m

# Run with faster progress updates (default: 10 seconds)
./bin/gql-extractor --domain="https://example.com" --progress=5s

# Use custom ports
make run DOMAIN="https://example.com" SELENIUM_PORT=5555 DEBUG_PORT=9333
```

### How to Use

1. Run the tool with your target domain
2. The browser will open automatically
3. Navigate through the website as needed - new pages will be processed automatically
4. Log in if needed - the tool will capture authenticated GraphQL requests
5. When done, simply close the browser window
6. Results will be saved automatically

### Progress Tracking

The tool provides real-time updates showing:

```
Progress Report [30s elapsed]:
  JS Files: 15 found, 12 downloaded, 10 processed
  Data: 3.45 MB downloaded
  GraphQL: 25 queries, 8 mutations found
  Network: 5 GraphQL requests captured
  Currently processing: https://example.com/js/main.chunk.js
```

## Output

The tool saves all extracted data to the `output/` folder and generates multiple files for comprehensive analysis:

### 1. SDL Format (`output/graphql_operations_example.com.graphql`)
Contains deduplicated GraphQL operations in standard SDL format:
```graphql
# Queries
query GetUser($id: ID!) {
  user(id: $id) {
    id
    name
    email
  }
}

# Mutations  
mutation CreateUser($input: CreateUserInput!) {
  createUser(input: $input) {
    id
    name
  }
}
```

### 2. JSON Format (`output/graphql_operations_example.com.json`)
Structured data with operation details, signatures, and inferred types:
```json
{
  "operations": [
    {
      "type": "query",
      "name": "GetUser",
      "variables": {"id": "ID!"},
      "fields": ["user"],
      "signature": "query GetUser($id: ID!)"
    }
  ],
  "summary": {
    "totalOperations": 15,
    "queries": 10,
    "mutations": 5
  },
  "inferredTypes": {
    "User": {
      "type": "Object",
      "fields": {
        "id": "String",
        "name": "String"
      }
    }
  }
}
```

### 3. Detailed Log (`output/graphql_operations_example.com_detailed.log`)
Complete capture information including:
- Static operations found in JavaScript
- Network captures with timestamps
- Request variables and responses
- Full operation bodies

## Makefile Commands

```bash
make run DOMAIN="..."     # Build and run the extractor
make stop                 # Stop ChromeDriver
make clean               # Remove build artifacts
make check-chrome        # Show detected Chrome version
make check-ports         # Check if ports are available
make logs               # View ChromeDriver logs
make help               # Show all commands
```

## Project Structure

```
.
├── capture.go          # Main extraction logic
├── parser.go          # GraphQL parsing and export functionality
├── Makefile           # Build automation
├── go.mod            # Go dependencies
├── .gitignore        # Git ignore rules
├── output/            # Extracted GraphQL data (gitignored)
└── bin/              # Build output
    ├── gql-extractor
    └── chromedriver/
```

## How It Works

1. **Browser Automation**: Uses Selenium WebDriver to control Chrome
2. **Network Monitoring**: Captures HTTP traffic via Chrome DevTools Protocol
3. **JavaScript Analysis**: Downloads and parses JS files for GraphQL queries
4. **Pattern Matching**: Uses regex to identify query and mutation patterns
5. **Continuous Processing**: Keeps processing new JS files as you navigate
6. **Progress Tracking**: Reports status in real-time
7. **Session Detection**: Automatically stops when you close the browser

## Troubleshooting

### ChromeDriver Version Mismatch
The tool automatically detects your Chrome version and downloads the matching ChromeDriver. If you still get version errors:

```bash
make check-chrome    # Check detected version
make clean          # Remove old ChromeDriver
make run DOMAIN="..." # Try again
```

### Port Already in Use
The tool automatically frees up required ports. If issues persist:

```bash
make stop           # Stop any running ChromeDriver
make check-ports    # Check port status
```

### No Queries Found
- Wait for the page to fully load (10 second delay by default)
- Navigate through different pages - the tool continues processing as you browse
- Check if the site uses GraphQL (look for /graphql endpoints)
- The tool will keep capturing until you close the browser

## Limitations

- Regex-based extraction may miss complex query formats
- Requires manual interaction for authenticated areas
- Only captures queries from loaded resources
- May not work with heavily obfuscated code

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `go test ./...`
5. Submit a pull request