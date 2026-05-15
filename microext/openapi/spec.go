package openapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Document struct {
	OpenAPI string               `json:"openapi"`
	Info    DocumentInfo         `json:"info"`
	Paths   map[string]*PathItem `json:"paths,omitempty"`
}

type DocumentInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type PathItem struct {
	Post *Operation `json:"post,omitempty"`
}

type Operation struct {
	OperationID string               `json:"operationId,omitempty"`
	Summary     string               `json:"summary,omitempty"`
	Description string               `json:"description,omitempty"`
	Tags        []string             `json:"tags,omitempty"`
	Parameters  []*Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody         `json:"requestBody,omitempty"`
	Responses   map[string]*Response `json:"responses,omitempty"`
	NATSSubject string               `json:"x-nats-subject,omitempty"`
}

type Parameter struct {
	Name     string          `json:"name"`
	In       string          `json:"in"`
	Required bool            `json:"required"`
	Schema   json.RawMessage `json:"schema,omitempty"`
	Desc     string          `json:"description,omitempty"`
}

type RequestBody struct {
	Required bool                  `json:"required"`
	Content  map[string]*MediaType `json:"content"`
}

type Response struct {
	Description string                `json:"description"`
	Content     map[string]*MediaType `json:"content,omitempty"`
}

type MediaType struct {
	Schema json.RawMessage `json:"schema"`
}

func subjectToPath(subject string, params []string) string {
	tokens := strings.Split(subject, ".")
	paramIdx := 0
	for i, tok := range tokens {
		if tok == "*" {
			var name string
			if paramIdx < len(params) {
				name = params[paramIdx]
			} else {
				name = fmt.Sprintf("param%d", paramIdx+1)
			}
			tokens[i] = "{" + name + "}"
			paramIdx++
		}
	}
	return "/" + strings.Join(tokens, "/")
}

func buildParameters(subject string, params []string) []*Parameter {
	tokens := strings.Split(subject, ".")
	var result []*Parameter
	paramIdx := 0
	for _, tok := range tokens {
		if tok != "*" {
			continue
		}
		name := fmt.Sprintf("param%d", paramIdx+1)
		if paramIdx < len(params) {
			name = params[paramIdx]
		}
		result = append(result, &Parameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   json.RawMessage(`{"type":"string"}`),
		})
		paramIdx++
	}
	return result
}
