// Copyright (c) 2020 Khramtsov Aleksei (contact@altsoftllc.com).
// This file (service.go at 24.06.2020, 15:26) is subject to the terms and
// conditions defined in file 'LICENSE', which is part of this project source code.
package generator

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/vetcher/go-astra/types"

	"github.com/tundrik/tg/pkg/tags"
	"github.com/tundrik/tg/pkg/utils"
)

type service struct {
	types.Interface

	log logrus.FieldLogger

	pkgPath string
	methods []*method
	tr      *Transport
	tags    tags.DocTags

	testsPath      string
	implementsPath string
}

func newService(log logrus.FieldLogger, tr *Transport, filePath string, iface types.Interface, options ...Option) (svc *service) {

	svc = &service{
		tr:        tr,
		log:       log,
		Interface: iface,
		tags:      tags.ParseTags(iface.Docs),
	}

	for _, option := range options {
		option(svc)
	}

	for _, method := range iface.Methods {
		svc.methods = append(svc.methods, newMethod(log, svc, method))
	}

	absPath, _ := filepath.Abs(filepath.Dir(filePath))
	svc.pkgPath, _ = utils.GetPkgPath(filepath.Dir(filePath), true)
	svc.pkgPath = path.Join(svc.pkgPath, path.Dir(strings.TrimPrefix(filePath, absPath)))

	return
}

func (svc service) isJsonRPC() bool {
	return svc.tags.IsSet(tagServerJsonRPC)
}

func (svc service) lcName() string {
	return strings.ToLower(svc.Name)
}

func (svc service) lccName() string {
	return utils.ToLowerCamel(svc.Name)
}

func (svc *service) renderClient(outDir string) (err error) {
	if svc.tags.Contains(tagServerJsonRPC) {
		err = svc.renderExchange(outDir)
		err = svc.renderClientJsonRPC(outDir)
	}
	return
}

func (svc *service) render(outDir string) (err error) {

	showError(svc.log, svc.renderHTTP(outDir), "renderHTTP")
	showError(svc.log, svc.renderServer(outDir), "renderServer")
	showError(svc.log, svc.renderExchange(outDir), "renderExchange")
	showError(svc.log, svc.renderMiddleware(outDir), "renderMiddleware")
	// showError(svc.log, svc.renderImplement(svc.implementsPath), "renderImplement")

	if svc.tags.Contains(tagTests) {
		showError(svc.log, svc.renderTest(svc.testsPath), "renderTest")
	}

	if svc.tags.Contains(tagTrace) {
		showError(svc.log, svc.renderTrace(outDir), "renderTrace")
	}
	if svc.tags.Contains(tagMetrics) {
		showError(svc.log, svc.renderMetrics(outDir), "renderMetrics")
	}
	if svc.tags.Contains(tagLogger) {
		showError(svc.log, svc.renderLogger(outDir), "renderLogger")
	}
	if svc.tags.Contains(tagServerJsonRPC) {
		showError(svc.log, svc.renderJsonRPC(outDir), "renderJsonRPC")
	}
	if svc.tags.Contains(tagServerHTTP) {
		showError(svc.log, svc.renderREST(outDir), "renderREST")
	}
	return
}

func (svc service) batchPath() string {
	return path.Join("/", svc.tr.tags.Value(tagHttpPrefix), svc.tags.Value(tagHttpPrefix), svc.tags.Value(tagHttpPath, path.Join("/", svc.lcName())))
}
