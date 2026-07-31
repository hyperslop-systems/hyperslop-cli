// Package doc embeds the hyperslop customer-facing documentation into the
// binary.
//
// The pages carry Glazed help frontmatter, so they are queryable by slug
// (`hyperslop help cli-output`), filterable by topic, and rendered with the
// same machinery as every other go-go-golems tool. They describe the
// agent/customer CLI surface: how to obtain a scoped token with the device
// pairing flow, and how the row-producing verbs select formats and project
// fields.
//
// The proprietary server's own documentation (the web workbench it hosts, the
// ZITADEL dev stack) lives in go-go-datalab/pkg/doc. The admin datalab binary
// loads both help sets so `datalab help cli-output` still resolves.
package doc

import (
	"embed"
	"io/fs"

	"github.com/go-go-golems/glazed/pkg/help"
)

//go:embed topics
var docFS embed.FS

// FS returns the embedded documentation tree.
func FS() fs.FS {
	return docFS
}

// AddDocToHelpSystem loads every embedded section into the given help system.
//
// Called once, from the CLI root. A duplicate slug is a load-time error rather
// than a silently shadowed page.
func AddDocToHelpSystem(helpSystem *help.HelpSystem) error {
	return helpSystem.LoadSectionsFromFS(docFS, ".")
}
