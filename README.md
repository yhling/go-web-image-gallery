# Go web image gallery

A simple Go based web server that turns your directories into a iCloud-like image gallery.

## Features

- Standalone executable. No DB, no frameworks, no containers.
- Supports viewing of almost every image format (including HEIC, DNG, ARW) on every browser.
- Supports iOS live photos
- Fast preview and thumbnail generation

## Usage

```bash
directory-server.exe -root "D:\Photos" -port 8080
```

**Arguments:**
```
- `-root`: Root directory to serve (if not specified defaults to current directory)
- `-port`: Port to listen on (default: 8080)
- `-base-path`: Base path for the application when behind a reverse proxy (e.g., `/gallery`) (default: empty)
```

On your browser go to:
```
http://localhost:8080/gallery
```

## Prerequisites

**Windows:**
None, all binaries are included in the zip

**macOS:**
```bash
brew install vips ffmpeg

```
**Ubuntu/Debian:**
```bash
sudo apt-get install libvips-dev ffmpeg
```

**Fedora:**
```bash
sudo dnf install vips-devel ffmpeg
```

## Build 
Mac/Linux
```bash
go build -o directory-server
```

Windows
```bash
go build -o directory-server.exe
```

