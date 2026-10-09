# Background for programmers new to GIS

You can write code and you've never touched a map projection. This page covers enough to
follow what `georef` does and why. What the component takes in and puts out lives in the
controlplane's [components.md](https://github.com/line-age/controlplane/blob/main/components.md#georef).

## What georeferencing is

A scanned map is a picture. It has pixels, not latitudes. Georeferencing is the job of
telling the computer where that picture sits on the earth, so a line drawn on a 1930s sheet
can be laid over a modern basemap, measured, and exported.

You do it with **control points** (GCPs): pairs of "this pixel on the scan" and "this
lon/lat on the earth". Give it enough pairs and a **transform** (a polynomial or a
thin-plate spline, usually) fits a function from pixel space to earth space. Warping the
image through that function is the georeferenced map.

That's the whole idea. The hard part is that old maps are wrong in interesting ways (hand
drafting, paper shrinkage, odd or unknown projections), so the transform is always a
compromise, and the quality of the compromise depends almost entirely on which points you
picked.

Start here:

- [Georeferencing](https://en.wikipedia.org/wiki/Georeferencing) on Wikipedia. Short.
- [Coordinate Reference Systems](https://docs.qgis.org/latest/en/docs/gentle_gis_introduction/coordinate_reference_systems.html)
  from the QGIS gentle introduction. Read this before anything else if "EPSG:4326" means
  nothing to you. (It's lon/lat on WGS 84. You will also see EPSG:3857, which is what web
  map tiles use. Axis order will bite you at least once.)
- [Overview of georeferencing](https://doc.esri.com/en/arcgis-pro/latest/help/data/imagery/overview-of-georeferencing.html)
  from Esri. Commercial product, but the clearest plain explanation I know of transform
  types and residuals.
- [Georeferencing in QGIS](https://mapping.share.library.harvard.edu/tutorials/arcgis-qgis/georeference/)
  from the Harvard Map Collection, or the
  [QGIS training lesson](https://docs.qgis.org/latest/en/docs/training_manual/forestry/map_georeferencing.html).
  Do one by hand. Twenty minutes of placing points yourself teaches more than this page.

## Choosing control points, and measuring error

We learned this the hard way in the Legend spike, and wrote it up in
[`docs/georeferencing.md`](https://github.com/Tight-Line/legend/blob/main/docs/georeferencing.md).
Read that; it's the real reference. The short version:

- **Extremities first, then a spread-out interior.** A transform interpolates inside the
  convex hull of its control points and extrapolates outside it. Outside is where it goes
  wrong, and nobody looks there. Recognizable places cluster (cartographers label the
  interesting bits), so "pick places you recognize" is the wrong rule.
- **Precise features beat labeled ones.** A state-boundary corner or graticule intersection
  beats a city dot, which beats a coastline.
- **Residuals are not accuracy.** The residual is the error *at* a control point after the
  fit. Add enough coefficients and it goes to zero (a thin-plate spline is exactly zero by
  construction) while the map between the points gets worse.
- **Use leave-one-out (LOO) error to pick the transform.** Drop one point, fit with the
  rest, see how far off the dropped point lands; repeat for every point. That measures how
  well the warp generalizes, which is the thing you care about. It's ordinary
  [cross-validation](https://en.wikipedia.org/wiki/Cross-validation_(statistics)#Leave-one-out_cross-validation)
  applied to a small regression problem.
- **Then look at the corners.** LOO picks the transform; it doesn't tell you where the
  warp is good. Render the sheet's edges over a basemap and check by eye.

On a spike sheet, the same 24-point count gave a LOO error of 73 km with our points and
11 km with points someone else placed on surveyed intersections. Point quality is the whole
game.

## IIIF

[IIIF](https://iiif.io/get-started/) (the International Image Interoperability Framework,
pronounced "triple-eye-eff") is a set of HTTP APIs for serving big images and describing
them. It exists because every library and museum used to build its own deep-zoom viewer
over its own image store, and none of them could see each other's collections. IIIF
standardizes the URLs and the JSON, so any viewer can open any institution's images.
[Why IIIF](https://iiif.io/get-started/why-iiif/) has the pitch;
[How IIIF works](https://iiif.io/get-started/how-iiif-works/) has the mechanics.

Two APIs matter to us:

- **[Image API](https://iiif.io/api/image/3.0/).** Pixels, addressed by URL:
  `{server}/{identifier}/{region}/{size}/{rotation}/{quality}.{format}`. Ask for
  `/full/max/0/default.jpg` and you get the whole scan; ask for a region at a size and you
  get just that tile. An `info.json` alongside describes dimensions and available tiles.
  This is what makes a 600-megapixel scan usable in a browser.
- **[Presentation API](https://iiif.io/api/presentation/3.0/).** JSON-LD that describes
  the object. A **Manifest** is one thing (a map, a book); it has **Canvases** (a page, a
  sheet); each Canvas has **Annotations** that put content on it, most commonly the image
  from an Image API service. Collections group Manifests.

The georeference annotation is a [IIIF extension](https://iiif.io/api/extension/georef/):
a W3C Web Annotation whose target is a IIIF image and whose body is the control points
(as GeoJSON) plus the transform. So "a georeferenced map" for us is really two things, a
IIIF image and an annotation that points at it. Neither one modifies the other.

### IIIF servers we can use

Many of the maps we want are already served over IIIF by the institutions that hold them
(the [Library of Congress](https://www.loc.gov/maps/), the
[David Rumsey Map Collection](https://www.davidrumsey.com/), most large university
libraries). For those we host nothing; we point at their Manifest.
[Finding IIIF resources](https://iiif.io/guides/finding_resources/) explains how to dig a
Manifest URL out of a collection's site.

For scans that aren't on IIIF anywhere, we run our own image server. The usual open-source
options (see the IIIF [image server list](https://iiif.io/get-started/image-servers/)):

- [Cantaloupe](https://cantaloupe-project.github.io/). Java, widely deployed, reads most
  formats. The default choice.
- [IIPImage](https://iipimage.sourceforge.io/). C++, very fast, happiest with pyramidal
  TIFF or JPEG 2000.
- [serverless-iiif](https://github.com/samvera/serverless-iiif). An AWS Lambda
  implementation from the Samvera community. Cheap for a small, mostly idle collection.

Which one is a `georef` build decision, not a background one.

## Allmaps, and where it fits

[Allmaps](https://allmaps.org/) is an open-source toolkit for georeferencing IIIF maps. It
only works with IIIF, which is why the section above matters. Its parts, and what we do
with each:

- **[Allmaps Editor](https://editor.allmaps.org/).** A browser app: paste a IIIF Manifest
  URL, draw a mask around the map area, place control points. We plan to self-host it as
  both the georeferencing tool and the review tool. Try it on any public map; it's the
  fastest way to understand what we're building. (Edits on the public instance are
  published CC0, so don't experiment on anything you'd mind seeing out there.)
- **Existing annotations.** People have already georeferenced a lot of IIIF maps in
  Allmaps, and those annotations are public and CC0. Before we georeference a sheet, we
  check. On one spike sheet the existing annotation beat ours by a factor of seven.
- **The annotation format.** Allmaps wrote the IIIF georeference extension above, so what
  the editor saves is the same standard record `georef` puts out.
- **The libraries.** [`@allmaps/transform`](https://dev.allmaps.org/docs/packages/transform)
  does the warping math (its docs have a good plain-language rundown of each transform
  type), and the rendering packages draw a warped IIIF image straight into MapLibre,
  OpenLayers or Leaflet. `trace` and `review` will probably use these rather than
  pre-warped GeoTIFFs. Developer docs are at [dev.allmaps.org](https://dev.allmaps.org/);
  source is [on GitHub](https://github.com/allmaps/allmaps).
- **[Viewer](https://viewer.allmaps.org/) and [tile server](https://tiles.allmaps.org/).**
  Look at a georeferenced map, or get it as XYZ tiles you can add in QGIS.

The piece Allmaps doesn't give us is the server the editor saves to. The editor talks to
Allmaps' own API over a ShareDB websocket, and that server isn't in their public repo, so
self-hosting the editor means writing a compatible annotation server. That's the real
build in `georef`.

## Other things worth a look

- [Introduction to Map Warper](https://programminghistorian.org/en/lessons/introduction-map-warper)
  from the Programming Historian. A different tool, the same workflow, explained for
  historians rather than GIS people.
- [IIIF training: annotations and maps](https://training.iiif.io/annotations/use_cases/allmaps.html).
  Walks through an Allmaps georeference end to end from the IIIF side.
- [QGIS Georeferencer](https://docs.qgis.org/latest/en/docs/user_manual/working_with_raster/georeferencer.html)
  reference. QGIS is what the research partner uses, and what the released data has to open
  in, so it's worth having installed regardless.
