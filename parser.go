package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// OperationType represents the type of GraphQL operation
type OperationType string

const (
	Query        OperationType = "query"
	Mutation     OperationType = "mutation"
	Subscription OperationType = "subscription"
)

// GraphQLOperation represents a parsed GraphQL operation
type GraphQLOperation struct {
	Type      OperationType          `json:"type"`
	Name      string                 `json:"name"`
	Variables map[string]string      `json:"variables,omitempty"`
	Fields    []string               `json:"fields"`
	Raw       string                 `json:"raw"`
}

// SchemaExport represents the exported schema structure
type SchemaExport struct {
	Operations []GraphQLOperation     `json:"operations"`
	Types      map[string]interface{} `json:"types,omitempty"`
	Timestamp  string                 `json:"timestamp"`
}

// ParseGraphQLOperation attempts to parse a GraphQL operation string
func ParseGraphQLOperation(operation string) (*GraphQLOperation, error) {
	operation = strings.TrimSpace(operation)
	
	// More robust regex patterns
	operationPattern := regexp.MustCompile(`(?s)^(query|mutation|subscription)\s+(\w+)?\s*(\([^)]*\))?\s*\{(.+)\}$`)
	variablePattern := regexp.MustCompile(`\$(\w+):\s*([^,\)]+)`)
	fieldPattern := regexp.MustCompile(`(\w+)(?:\s*\([^)]*\))?\s*(?:\{[^}]*\})?`)
	
	matches := operationPattern.FindStringSubmatch(operation)
	if len(matches) < 5 {
		return nil, fmt.Errorf("invalid GraphQL operation format")
	}
	
	op := &GraphQLOperation{
		Type:      OperationType(matches[1]),
		Name:      matches[2],
		Variables: make(map[string]string),
		Fields:    []string{},
		Raw:       operation,
	}
	
	// Parse variables
	if matches[3] != "" {
		varMatches := variablePattern.FindAllStringSubmatch(matches[3], -1)
		for _, vm := range varMatches {
			if len(vm) >= 3 {
				op.Variables[vm[1]] = strings.TrimSpace(vm[2])
			}
		}
	}
	
	// Parse fields (simplified - just top level)
	body := matches[4]
	fieldMatches := fieldPattern.FindAllStringSubmatch(body, -1)
	for _, fm := range fieldMatches {
		if len(fm) >= 2 && fm[1] != "" {
			op.Fields = append(op.Fields, fm[1])
		}
	}
	
	return op, nil
}

// ExtractOperationsFromJS extracts GraphQL operations from JavaScript content with better parsing
func ExtractOperationsFromJS(content string) ([]*GraphQLOperation, error) {
	var operations []*GraphQLOperation
	
	// Improved patterns to handle minified code and template literals
	patterns := []string{
		// Standard GraphQL operations
		`(?s)(query|mutation|subscription)\s+\w+\s*\([^)]*\)\s*\{[^}]+(?:\{[^}]+\})*[^}]+\}`,
		// Operations without names
		`(?s)(query|mutation|subscription)\s*\([^)]*\)\s*\{[^}]+(?:\{[^}]+\})*[^}]+\}`,
		// Operations without variables
		`(?s)(query|mutation|subscription)\s+\w+\s*\{[^}]+(?:\{[^}]+\})*[^}]+\}`,
		// Template literal operations
		`gql\s*` + "`" + `\s*((?:query|mutation|subscription)[^` + "`" + `]+)` + "`",
		// Escaped in strings
		`["']\\n\s*((?:query|mutation|subscription)[^"']+)["']`,
	}
	
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(content, -1)
		
		for _, match := range matches {
			var opString string
			if len(match) > 1 {
				if match[1] == "query" || match[1] == "mutation" || match[1] == "subscription" {
					opString = match[0]
				} else {
					opString = match[1]
				}
			} else {
				opString = match[0]
			}
			
			// Clean up escaped characters
			opString = strings.ReplaceAll(opString, "\\n", "\n")
			opString = strings.ReplaceAll(opString, "\\t", "  ")
			opString = strings.ReplaceAll(opString, `\"`, `"`)
			
			// Try to parse
			op, err := ParseGraphQLOperation(opString)
			if err == nil && op != nil {
				operations = append(operations, op)
			}
		}
	}
	
	return operations, nil
}

// ExportToSDL converts operations to GraphQL SDL format
func ExportToSDL(operations []*GraphQLOperation) string {
	var sdl strings.Builder
	
	sdl.WriteString("# Extracted GraphQL Operations\n")
	sdl.WriteString("# Generated at: " + time.Now().Format(time.RFC3339) + "\n\n")
	
	// Group by type
	queries := []*GraphQLOperation{}
	mutations := []*GraphQLOperation{}
	subscriptions := []*GraphQLOperation{}
	
	for _, op := range operations {
		switch op.Type {
		case Query:
			queries = append(queries, op)
		case Mutation:
			mutations = append(mutations, op)
		case Subscription:
			subscriptions = append(subscriptions, op)
		}
	}
	
	// Write operations
	if len(queries) > 0 {
		sdl.WriteString("# Queries\n")
		for _, op := range queries {
			sdl.WriteString(formatOperationSDL(op))
			sdl.WriteString("\n\n")
		}
	}
	
	if len(mutations) > 0 {
		sdl.WriteString("# Mutations\n")
		for _, op := range mutations {
			sdl.WriteString(formatOperationSDL(op))
			sdl.WriteString("\n\n")
		}
	}
	
	if len(subscriptions) > 0 {
		sdl.WriteString("# Subscriptions\n")
		for _, op := range subscriptions {
			sdl.WriteString(formatOperationSDL(op))
			sdl.WriteString("\n\n")
		}
	}
	
	return sdl.String()
}

// formatOperationSDL formats a single operation in SDL
func formatOperationSDL(op *GraphQLOperation) string {
	// If we have the raw operation with proper formatting, use it
	if op.Raw != "" && strings.Contains(op.Raw, "\n") {
		return op.Raw
	}
	
	// Otherwise, reconstruct from parsed components
	var sb strings.Builder
	
	sb.WriteString(string(op.Type))
	if op.Name != "" {
		sb.WriteString(" " + op.Name)
	}
	
	if len(op.Variables) > 0 {
		sb.WriteString("(")
		first := true
		for name, typ := range op.Variables {
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString("$" + name + ": " + typ)
			first = false
		}
		sb.WriteString(")")
	}
	
	sb.WriteString(" {\n")
	
	// If we have the raw operation, try to extract the body with proper formatting
	if op.Raw != "" {
		// Extract the body from the raw operation
		if bodyStart := strings.Index(op.Raw, "{"); bodyStart != -1 {
			if bodyEnd := strings.LastIndex(op.Raw, "}"); bodyEnd != -1 && bodyEnd > bodyStart {
				body := op.Raw[bodyStart+1 : bodyEnd]
				lines := strings.Split(body, "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line != "" {
						sb.WriteString("  " + line + "\n")
					}
				}
			}
		}
	} else {
		// Fall back to simple field list
		for _, field := range op.Fields {
			sb.WriteString("  " + field + "\n")
		}
	}
	
	sb.WriteString("}")
	
	return sb.String()
}

// ExportToJSON exports operations as JSON with detailed information
func ExportToJSON(operations []*GraphQLOperation, captures []GraphQLCapture) ([]byte, error) {
	// Convert operations to include more details
	detailedOps := make([]map[string]interface{}, 0, len(operations))
	
	for _, op := range operations {
		detailedOp := map[string]interface{}{
			"type":      op.Type,
			"name":      op.Name,
			"variables": op.Variables,
			"fields":    op.Fields,
			"signature": extractOperationSignature(op),
		}
		
		// Add variable types if available
		if len(op.Variables) > 0 {
			varTypes := make(map[string]interface{})
			for name, typ := range op.Variables {
				varTypes[name] = map[string]interface{}{
					"type":     typ,
					"required": strings.HasSuffix(typ, "!"),
				}
			}
			detailedOp["variableTypes"] = varTypes
		}
		
		detailedOps = append(detailedOps, detailedOp)
	}
	
	export := map[string]interface{}{
		"operations": detailedOps,
		"timestamp":  time.Now().Format(time.RFC3339),
		"summary": map[string]interface{}{
			"totalOperations": len(operations),
			"queries":         countOperationType(operations, Query),
			"mutations":       countOperationType(operations, Mutation),
			"subscriptions":   countOperationType(operations, Subscription),
		},
	}
	
	// Try to infer types from responses
	types := make(map[string]interface{})
	for _, capture := range captures {
		if capture.Response != nil {
			// Basic type inference from responses
			if respMap, ok := capture.Response.(map[string]interface{}); ok {
				for key, value := range respMap {
					inferredType := inferTypeStructure(value)
					if inferred, ok := inferredType.(map[string]interface{}); ok {
						types[key] = inferred
					} else {
						types[key] = inferredType
					}
				}
			}
		}
	}
	
	if len(types) > 0 {
		export["inferredTypes"] = types
	}
	
	return json.MarshalIndent(export, "", "  ")
}

// inferType attempts to infer GraphQL type from response data
func inferType(value interface{}) string {
	switch v := value.(type) {
	case string:
		return "String"
	case float64:
		if v == float64(int(v)) {
			return "Int"
		}
		return "Float"
	case bool:
		return "Boolean"
	case []interface{}:
		if len(v) > 0 {
			return "[" + inferType(v[0]) + "]"
		}
		return "[Unknown]"
	case map[string]interface{}:
		return "Object"
	case nil:
		return "Null"
	default:
		return "Unknown"
	}
}

// inferTypeStructure attempts to infer detailed type structure from response data
func inferTypeStructure(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		fields := make(map[string]interface{})
		for key, val := range v {
			fields[key] = inferTypeStructure(val)
		}
		return map[string]interface{}{
			"type":   "Object",
			"fields": fields,
		}
	case []interface{}:
		if len(v) > 0 {
			return map[string]interface{}{
				"type": "List",
				"of":   inferTypeStructure(v[0]),
			}
		}
		return map[string]interface{}{
			"type": "List",
			"of":   "Unknown",
		}
	default:
		return inferType(value)
	}
}

// extractOperationSignature creates a signature string for an operation
func extractOperationSignature(op *GraphQLOperation) string {
	var sig strings.Builder
	
	sig.WriteString(string(op.Type))
	if op.Name != "" {
		sig.WriteString(" " + op.Name)
	}
	
	if len(op.Variables) > 0 {
		sig.WriteString("(")
		first := true
		for name, typ := range op.Variables {
			if !first {
				sig.WriteString(", ")
			}
			sig.WriteString("$" + name + ": " + typ)
			first = false
		}
		sig.WriteString(")")
	}
	
	return sig.String()
}

// countOperationType counts operations of a specific type
func countOperationType(operations []*GraphQLOperation, opType OperationType) int {
	count := 0
	for _, op := range operations {
		if op.Type == opType {
			count++
		}
	}
	return count
}

// DeduplicateOperations removes duplicate GraphQL operations based on their content
func DeduplicateOperations(operations []*GraphQLOperation) []*GraphQLOperation {
	seen := make(map[string]bool)
	unique := make([]*GraphQLOperation, 0)
	
	for _, op := range operations {
		// Create a unique key based on the operation's content
		key := createOperationKey(op)
		
		if !seen[key] {
			seen[key] = true
			unique = append(unique, op)
		}
	}
	
	return unique
}

// createOperationKey creates a unique key for an operation to detect duplicates
func createOperationKey(op *GraphQLOperation) string {
	// Normalize the raw operation for comparison
	normalized := normalizeGraphQL(op.Raw)
	
	// If raw is empty, create key from components
	if normalized == "" {
		var key strings.Builder
		key.WriteString(string(op.Type))
		key.WriteString("|")
		key.WriteString(op.Name)
		key.WriteString("|")
		
		// Sort variables for consistent key
		if len(op.Variables) > 0 {
			varKeys := make([]string, 0, len(op.Variables))
			for k := range op.Variables {
				varKeys = append(varKeys, k)
			}
			// Simple string sort
			for i := range varKeys {
				for j := i + 1; j < len(varKeys); j++ {
					if varKeys[i] > varKeys[j] {
						varKeys[i], varKeys[j] = varKeys[j], varKeys[i]
					}
				}
			}
			
			for _, k := range varKeys {
				key.WriteString(k)
				key.WriteString(":")
				key.WriteString(op.Variables[k])
				key.WriteString(",")
			}
		}
		
		// Sort fields for consistent key
		fields := make([]string, len(op.Fields))
		copy(fields, op.Fields)
		for i := range fields {
			for j := i + 1; j < len(fields); j++ {
				if fields[i] > fields[j] {
					fields[i], fields[j] = fields[j], fields[i]
				}
			}
		}
		
		for _, field := range fields {
			key.WriteString("|")
			key.WriteString(field)
		}
		
		return key.String()
	}
	
	return normalized
}

// normalizeGraphQL normalizes a GraphQL operation string for comparison
func normalizeGraphQL(query string) string {
	// Remove comments
	commentPattern := regexp.MustCompile(`#[^\n]*`)
	query = commentPattern.ReplaceAllString(query, "")
	
	// Normalize whitespace
	query = strings.TrimSpace(query)
	query = regexp.MustCompile(`\s+`).ReplaceAllString(query, " ")
	
	// Remove spaces around punctuation
	query = regexp.MustCompile(`\s*([\{\}\(\)\[\]:,])\s*`).ReplaceAllString(query, "$1")
	
	return query
}