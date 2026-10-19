# georef

Lineage IIIF serving, self-hosted Allmaps, and annotation server.

Part of [Lineage](https://github.com/line-age/controlplane). What this component takes in,
what it puts out, and what it depends on are described in the controlplane's
[components.md](https://github.com/line-age/controlplane/blob/main/components.md#georef);
this README will say how it is built once it is.

New to GIS, IIIF or Allmaps? Start with [docs/background.md](docs/background.md).

Work is tracked in the [Georef](https://github.com/orgs/line-age/projects/3) project. The
team lead is [@nickmarden](https://github.com/nickmarden). If that changes, the
[controlplane's table](https://github.com/line-age/controlplane#projects-and-leads) is updated first.

## Architecture

Everything a map needs, from scan to georeference, runs inside `georef`. Nothing depends
on allmaps.org; existing annotations there can be imported but are never required.

```mermaid
flowchart LR
    UP([uploader]) -->|upload, LDAP login| IIIF
    subgraph georef
        IIIF[IIIF image service<br/>originals + tiles]
        EDITOR[Allmaps editor]
        ANNO[annotation server]
        DB[(Postgres<br/>Georeference Annotations,<br/>every version)]
        TILES[Allmaps Tile Server<br/>+ Preview]
        VIEWER[Allmaps Viewer]
        IIIF -->|image and manifest URLs| EDITOR
        EDITOR <-->|ShareDB websocket + REST| ANNO
        ANNO --> DB
        ANNO -->|annotations| TILES
        IIIF -->|IIIF tiles| TILES
        ANNO -->|annotations| VIEWER
    end
    GEO([georeferencer]) -->|LDAP login| EDITOR
    IIIF -->|IIIF tiles, public unless the scan's rights are in question| VIEW([IIIF viewers])
    TILES -->|warped XYZ tiles| QGIS([QGIS])
    ANNO -->|annotation permalinks, public| OUT([viewers, store])
    AM[(allmaps.org)] -.->|import an existing annotation| ANNO
```

- **IIIF image service** ([#1](https://github.com/line-age/georef/issues/1)). Uploading
  needs an LDAP login. Anyone can download, except for scans whose copyright is in
  question; those need a login too.
- **Allmaps editor** ([#2](https://github.com/line-age/georef/issues/2)). It opens sheets
  by their IIIF URLs from the image service. Editing needs an LDAP login.
- **Annotation server** (#2). It answers the editor on three APIs (a ShareDB websocket,
  REST and annotations) and stores what the editor saves: one Georeference Annotation
  per sheet, in the IIIF georeference extension format. Each annotation holds the
  sheet's control points (pixel position paired with longitude and latitude), the mask
  around the map area, and the transformation type. Every saved version is kept, with
  who saved it and when.
- **Allmaps Tile Server, Preview and Viewer**
  ([#5](https://github.com/line-age/georef/issues/5)). The Tile Server turns an
  annotation and the image service's tiles into warped XYZ map tiles, which is how a
  georeferenced sheet opens in QGIS. Preview makes thumbnails; the Viewer shows warped
  maps in the browser. allmaps.org runs all three on Cloudflare; we run our own.

Allmaps publishes the editor, viewer, tile server and its database schema, but not the
API server, and nothing builds for containers today. We're asking upstream before
building ([#4](https://github.com/line-age/georef/issues/4)): what we write for
self-hosting should go back to Allmaps where they'll take it.

Nothing here yet.
