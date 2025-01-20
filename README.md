# GraphQL Query Extractor

A dynamic GraphQL query and mutation extraction tool that captures queries from JavaScript files loaded during browser sessions. The tool supports authenticated sessions and real-time extraction of GraphQL operations as JavaScript files are loaded.

## Features

- Real-time monitoring of JavaScript files loaded during browsing
- Extraction of GraphQL queries and mutations using pattern matching
- Support for authenticated sessions through manual browser interaction
- Automatic saving of extracted queries to a schema file

## Prerequisites

- Go
- Chrome/Chromium browser
- ChromeDriver matching your Chrome version
- Required Go packages:
  - github.com/mafredri/cdp
  - github.com/mafredri/cdp/rpcc
  - github.com/tebeka/selenium

## Installation

1. Clone the repository
2. Install dependencies:
```bash
go get github.com/mafredri/cdp
go get github.com/mafredri/cdp/rpcc
go get github.com/tebeka/selenium
```

3. Download ChromeDriver for your platform matching your Chrome version from:
   https://sites.google.com/chromium.org/driver/

## Setup

1. Start ChromeDriver with the required ports:
```bash
./chromedriver --port=4444 --remote-debugging-port=9222
```

2. Build the tool:
```bash
go build -o gql-extractor
```

## Usage

1. Start the tool with a target domain:
```bash
./gql-extractor --domain="https://example.com"
```

2. When the browser opens:
   - Perform any required authentication
   - Navigate through the application as needed
   - The tool will automatically capture GraphQL queries from loaded JS files

3. Extracted queries will be saved to: `graphql_queries_mutations_domain_name.graphql`

## Output

The tool generates a .graphql file containing all unique queries and mutations found in the loaded JavaScript files. The filename is automatically generated based on the target domain:
```
graphql_queries_mutations_example.com.graphql
```

## Notes

- The tool opens a visible Chrome instance to allow for user interaction
- Queries are extracted using regex pattern matching for both queries and mutations
- Results are appended to the output file, allowing for multiple runs
- JavaScript files are processed as they are loaded during the browsing session

## Limitations

- Regex-based extraction may not catch all query variants
- Requires manual browser interaction for authenticated sessions
- Only extracts queries from loaded JavaScript files
- Does not handle minified/obfuscated JavaScript optimally

## Contributing

Feel free to submit issues and enhancement requests.
