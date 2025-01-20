# GraphQL Query Extractor

<p align="center">
  <img width="400" alt="Screenshot 2025-01-20 at 10 51 50 AM" src="https://github.com/user-attachments/assets/bc5031e7-5838-479c-a1bc-759052762683" />
</p>

A dynamic GraphQL query and mutation extraction tool that captures queries from JavaScript files loaded during browser sessions. The tool supports authenticated sessions and real-time extraction of GraphQL operations as JavaScript files are loaded.

## Features

- Real-time monitoring of JavaScript files loaded during browsing
- Extraction of GraphQL queries and mutations using pattern matching
- Support for authenticated sessions through manual browser interaction
- Automatic saving of extracted queries to a schema file

## Prerequisites

- Go
- Chrome/Chromium browser (version 132.0.6834.83 or compatible)

## Quick Start

1. Clone the repository:
```bash
git clone https://github.com/yourusername/gql-extractor
cd gql-extractor
```

2. Build and setup:
```bash
make all
```

3. Run the tool:
```bash
make run DOMAIN="https://example.com"
```

4. When the browser opens:
   - Perform any required authentication
   - Navigate through the application as needed
   - The tool will automatically capture GraphQL queries from loaded JS files


## Output

The tool generates a `.graphql` file containing all unique queries and mutations found in the loaded JavaScript files. The filename is automatically generated based on the target domain:
```
graphql_queries_mutations_example.com.graphql
```

## Project Structure
```
.
├── Makefile             # Build and automation scripts
├── go.mod               # Go module definition
├── go.sum               # Go module checksums
├── main.go              # Main application code
└── bin/                 # Built binaries and ChromeDriver
    ├── gql-extractor
    └── chromedriver/
```

## Make Commands

- `make all`: Install dependencies, build the tool, and download ChromeDriver
- `make build`: Build the Go binary
- `make deps`: Install Go dependencies
- `make run DOMAIN="https://example.com"`: Run the tool against a specific domain
- `make stop`: Stop the ChromeDriver process
- `make clean`: Remove built binaries and downloads

## Manual Build

If you prefer to build manually:

1. Initialize Go modules:
```bash
go mod init
go mod tidy
```

2. Build the binary:
```bash
go build -o bin/gql-extractor
```

3. Download the appropriate ChromeDriver version from:
   https://storage.googleapis.com/chrome-for-testing-public/132.0.6834.83/

## Limitations

- Regex-based extraction may not catch all query variants
- Requires manual browser interaction for authenticated sessions
- Only extracts queries from loaded JavaScript files
- Does not handle minified/obfuscated JavaScript optimally

## Troubleshooting

1. ChromeDriver version mismatch:
   - Ensure Chrome browser version matches ChromeDriver version (132.0.6834.83)
   - Update CHROMEDRIVER_VERSION in Makefile if needed

2. Port conflicts:
   - Default ports used: 4444 (ChromeDriver) and 9222 (DevTools)
   - Ensure these ports are available or modify in main.go

3. Permission issues:
   - On Unix systems, ensure chromedriver is executable:
   ```bash
   chmod +x bin/chromedriver/chromedriver-*/chromedriver
   ```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request
