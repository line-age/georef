// Package iiif builds IIIF Presentation API v3 manifests.
// It has no I/O dependencies; callers supply all required data.
package iiif

import (
	"fmt"

	"github.com/line-age/georef/internal/model"
)

// LanguageMap is the IIIF v3 language-keyed string map, e.g. {"none": ["My Title"]}.
type LanguageMap map[string][]string

// Manifest is a IIIF Presentation API v3 manifest.
type Manifest struct {
	Context string      `json:"@context"`
	ID      string      `json:"id"`
	Type    string      `json:"type"`
	Label   LanguageMap `json:"label"`
	Items   []Canvas    `json:"items"`
}

// Canvas represents one image in the manifest (one sheet = one canvas).
type Canvas struct {
	ID     string           `json:"id"`
	Type   string           `json:"type"`
	Width  int              `json:"width"`
	Height int              `json:"height"`
	Items  []AnnotationPage `json:"items"`
}

// AnnotationPage groups the painting annotations for a canvas.
type AnnotationPage struct {
	ID    string       `json:"id"`
	Type  string       `json:"type"`
	Items []Annotation `json:"items"`
}

// Annotation is a painting annotation that places an image body onto its canvas.
type Annotation struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Motivation string    `json:"motivation"`
	Body       ImageBody `json:"body"`
	Target     string    `json:"target"`
}

// ImageBody is the image referenced by a painting annotation.
type ImageBody struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Format  string         `json:"format"`
	Width   int            `json:"width"`
	Height  int            `json:"height"`
	Service []ImageService `json:"service"`
}

// ImageService is a IIIF Image API v2 service descriptor embedded in an image body.
// It uses @id/@type (the v2 key names) even inside a v3 manifest, per the IIIF spec
// on embedding services from older API versions.
type ImageService struct {
	ID      string `json:"@id"`
	Type    string `json:"@type"`
	Profile string `json:"profile"`
}

// Build constructs a IIIF Presentation v3 manifest for the given sheet.
// publicBaseURL is the external root (e.g. "https://maps.example.com").
// imageAPIVersion is the IIIF Image API version (e.g. 2).
// imageAPILevel is the IIIF Image API compliance level (e.g. 2).
// width and height are the pixel dimensions of the sheet image, obtained from info.json.
func Build(sheet *model.Sheet, publicBaseURL string, imageAPIVersion, imageAPILevel, width, height int) *Manifest {
	iiifBase := publicBaseURL + "/iiif/" + sheet.ID
	canvasID := iiifBase + "/canvas/p1"

	serviceType := fmt.Sprintf("ImageService%d", imageAPIVersion)
	serviceProfile := fmt.Sprintf("http://iiif.io/api/image/%d/level%d.json", imageAPIVersion, imageAPILevel)

	return &Manifest{
		Context: "http://iiif.io/api/presentation/3/context.json",
		ID:      iiifBase + "/manifest",
		Type:    "Manifest",
		Label:   LanguageMap{"none": {sheet.Title}},
		Items: []Canvas{
			{
				ID:     canvasID,
				Type:   "Canvas",
				Width:  width,
				Height: height,
				Items: []AnnotationPage{
					{
						ID:   canvasID + "/page",
						Type: "AnnotationPage",
						Items: []Annotation{
							{
								ID:         canvasID + "/painting",
								Type:       "Annotation",
								Motivation: "painting",
								Body: ImageBody{
									ID:     iiifBase + "/full/max/0/default.jpg",
									Type:   "Image",
									Format: "image/jpeg",
									Width:  width,
									Height: height,
									Service: []ImageService{
										{
											ID:      iiifBase,
											Type:    serviceType,
											Profile: serviceProfile,
										},
									},
								},
								Target: canvasID,
							},
						},
					},
				},
			},
		},
	}
}
