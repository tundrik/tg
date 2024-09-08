// Copyright (c) 2020 Khramtsov Aleksei (contact@altsoftllc.com).
// This file (swagger.go at 25.06.2020, 0:38) is subject to the terms and
// conditions defined in file 'LICENSE', which is part of this project source code.
package generator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/valyala/fasthttp"
	"gopkg.in/yaml.v3"

	"github.com/tundrik/tg/pkg/tags"
	"github.com/tundrik/tg/pkg/utils"
)

const (
	contentJSON      = "application/json"
	contentMultipart = "multipart/form-data"
)

type swagger struct {
	*Transport

	schemas    swSchemas
	aliasTypes map[string]int
	knownTypes map[string]int
}

func newSwagger(tr *Transport) (doc *swagger) {

	doc = &swagger{
		Transport:  tr,
		schemas:    make(swSchemas),
		aliasTypes: make(map[string]int),
		knownTypes: make(map[string]int),
	}
	return
}

func (doc *swagger) render(outFilePath string) (err error) {

	if err = os.MkdirAll(filepath.Dir(outFilePath), 0777); err != nil {
		return
	}

	var swaggerDoc swObject
	swaggerDoc.OpenAPI = "3.0.0"
	swaggerDoc.Info.Title = doc.tags.Value("title")
	swaggerDoc.Info.Version = doc.tags.Value("version")
	swaggerDoc.Info.Description = doc.tags.Value("description")
	swaggerDoc.Paths = make(map[string]swPath)
	tagServers := strings.Split(doc.tags.Value("servers"), "|")
	for _, tagServer := range tagServers {
		var serverDesc string
		serverValues := strings.Split(tagServer, ";")

		if len(serverValues) > 1 {
			serverDesc = serverValues[1]
		}
		swaggerDoc.Servers = append(swaggerDoc.Servers, swServer{URL: serverValues[0], Description: serverDesc})
	}
	for _, serviceName := range doc.serviceKeys() {
		service := doc.services[serviceName]
		serviceTags := []string{service.Name}
		serviceTags = strings.Split(service.tags.Value(tagSwaggerTags, service.Name), ",")
		doc.log.WithField("module", "swagger").Infof("service %s append jsonRPC methods", serviceTags)
		for _, method := range service.methods {
			if method.tags.Contains(tagSwaggerTags) {
				serviceTags = strings.Split(method.tags.Value(tagSwaggerTags), ",")
			}
			successCode := method.tags.ValueInt(tagHttpSuccess, fasthttp.StatusOK)

			doc.registerStruct(method.requestStructName(), service.pkgPath, method.tags, method.argumentsWithUploads())
			doc.registerStruct(method.responseStructName(), service.pkgPath, method.tags, method.results())

			var parameters []swParameter
			var retHeaders map[string]swHeader
			for argName, headerKey := range method.varHeaderMap() {
				if arg := method.argByName(argName); arg != nil {
					parameters = append(parameters, swParameter{
						In:       "header",
						Name:     headerKey,
						Required: true,
						Schema:   doc.walkVariable(arg.Name, service.pkgPath, arg.Type, nil),
					})
				}
				if ret := method.resultByName(argName); ret != nil {
					if retHeaders == nil {
						retHeaders = make(map[string]swHeader)
					}
					retHeaders[headerKey] = swHeader{
						Schema: doc.walkVariable(ret.Name, service.pkgPath, ret.Type, nil),
					}
				}
			}
			for argName, headerKey := range method.argPathMap() {
				if arg := method.argByName(argName); arg != nil {
					parameters = append(parameters, swParameter{
						In:       "path",
						Name:     headerKey,
						Required: true,
						Schema:   doc.walkVariable(arg.Name, service.pkgPath, arg.Type, nil),
					})
				}
				if ret := method.resultByName(argName); ret != nil {
					if retHeaders == nil {
						retHeaders = make(map[string]swHeader)
					}
					retHeaders[headerKey] = swHeader{
						Schema: doc.walkVariable(ret.Name, service.pkgPath, ret.Type, nil),
					}
				}
			}
			for argName, cookieName := range method.varCookieMap() {
				if arg := method.argByName(argName); arg != nil {
					parameters = append(parameters, swParameter{
						In:       "cookie",
						Name:     cookieName,
						Required: true,
						Schema:   doc.walkVariable(arg.Name, service.pkgPath, arg.Type, nil),
					})
				}
				if ret := method.resultByName(argName); ret != nil {

					if retHeaders == nil {
						retHeaders = make(map[string]swHeader)
					}
					retHeaders["Set-Cookie"] = swHeader{
						Description: cookieName,
						Schema:      doc.walkVariable(ret.Name, service.pkgPath, ret.Type, nil),
					}
				}
			}
			if service.tags.Contains(tagServerJsonRPC) && !method.tags.Contains(tagMethodHTTP) {
				postMethod := &swOperation{
					Summary:     method.tags.Value(tagSummary),
					Description: method.tags.Value(tagDesc),
					Parameters:  parameters,
					Tags:        serviceTags,
					Deprecated:  method.tags.Contains(tagDeprecated),
					RequestBody: &swRequestBody{
						Content: swContent{
							contentJSON: swMedia{Schema: jsonrpcSchema("params", swSchema{Ref: "#/components/schemas/" + method.requestStructName()})},
						},
					},
					Responses: swResponses{
						"200": swResponse{
							Description: codeToText(200),
							Headers:     retHeaders,
							Content: swContent{
								contentJSON: swMedia{Schema: swSchema{
									OneOf: []swSchema{
										jsonrpcSchema("result", swSchema{Ref: "#/components/schemas/" + method.responseStructName()}),
										jsonrpcErrorSchema(),
									},
								},
								},
							},
						},
					},
				}
				swaggerDoc.Paths[method.jsonrpcPath()] = swPath{Post: postMethod}
			} else if service.tags.Contains(tagServerHTTP) && method.tags.Contains(tagMethodHTTP) {
				doc.log.WithField("module", "swagger").Infof("service %s append HTTP method %s", serviceTags, method.Name)
				httpValue, found := swaggerDoc.Paths[method.jsonrpcPath()]
				if !found {
					swaggerDoc.Paths[method.httpPath()] = swPath{}
				}
				requestContentType := contentJSON
				responseContentType := contentJSON
				if method.tags.Contains(tagUploadVars) {
					requestContentType = contentMultipart
				}
				httpMethod := &swOperation{
					Summary:     method.tags.Value(tagSummary),
					Description: method.tags.Value(tagDesc),
					Parameters:  parameters,
					Tags:        serviceTags,
					Deprecated:  method.tags.Contains(tagDeprecated),
					RequestBody: &swRequestBody{
						Content: doc.clearContent(swContent{
							requestContentType: swMedia{Schema: swSchema{Ref: "#/components/schemas/" + method.requestStructName()}},
						}),
					},
					Responses: swResponses{
						fmt.Sprintf("%d", successCode): swResponse{
							Description: codeToText(successCode),
							Headers:     retHeaders,
							Content: doc.clearContent(swContent{
								responseContentType: swMedia{Schema: swSchema{Ref: "#/components/schemas/" + method.responseStructName()}},
							}),
						},
					},
				}
				var methodTags tags.DocTags
				doc.fillErrors(httpMethod.Responses, methodTags.Merge(service.tags).Merge(method.tags))

				if httpMethod.RequestBody.Content == nil {
					httpMethod.RequestBody = nil
				}
				reflect.ValueOf(&httpValue).Elem().FieldByName(utils.ToCamel(strings.ToLower(method.httpMethod()))).Set(reflect.ValueOf(httpMethod))
				swaggerDoc.Paths[method.httpPath()] = httpValue
			}
		}
	}
	var docData []byte
	swaggerDoc.Components.Schemas = doc.schemas
	if strings.ToLower(filepath.Ext(outFilePath)) == ".json" {
		if docData, err = json.MarshalIndent(swaggerDoc, " ", "    "); err != nil {
			return
		}
	} else {
		if docData, err = yaml.Marshal(swaggerDoc); err != nil {
			return
		}
	}
	doc.log.Info("write to ", outFilePath)
	return ioutil.WriteFile(outFilePath, docData, 0600)
}

func (doc *swagger) fillErrors(responses swResponses, tags tags.DocTags) {

	for key, value := range tags {

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		code, _ := strconv.Atoi(key)

		var content swContent
		var pkgPath, typeName string

		if text, found := statusText[code]; found {

			if value == "skip" {
				continue
			}

			if value != "" {
				if tokens := strings.Split(value, ":"); len(tokens) == 2 {

					pkgPath = tokens[0]
					typeName = tokens[1]

					if retType := doc.searchType(pkgPath, typeName); retType != nil {
						content = swContent{contentJSON: swMedia{Schema: doc.walkVariable(typeName, pkgPath, retType, nil)}}
					}
				}
			}
			responses[key] = swResponse{Description: text, Content: content}

		} else if key == "defaultError" {

			if value != "" {
				if tokens := strings.Split(value, ":"); len(tokens) == 2 {

					pkgPath = tokens[0]
					typeName = tokens[1]

					if retType := doc.searchType(pkgPath, typeName); retType != nil {
						content = swContent{contentJSON: swMedia{Schema: doc.walkVariable(typeName, pkgPath, retType, nil)}}
					}
				}
			}
			responses["default"] = swResponse{Description: "Generic error", Content: content}
		}
	}
}

func (doc *swagger) clearContent(content swContent) swContent {

	for mime, media := range content {

		if media.Schema.Type == "object" && len(media.Schema.Properties) == 0 {
			delete(content, mime)
		}

		if media.Schema.Ref != "" {
			if schema, found := doc.schemas[strings.TrimPrefix(media.Schema.Ref, "#/components/schemas/")]; !found || len(schema.Properties) == 0 {
				delete(content, mime)
			}
		}
	}
	if len(content) == 0 {
		return nil
	}
	return content
}
