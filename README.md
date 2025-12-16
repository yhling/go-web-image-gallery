# YAIG - Yet Another Image Gallery (with HEIC support!)

A super fast zero-dependency Go based web server that serves your directories as a image gallery.

## Features

- Standalone executable, click to run. No DB, no frameworks to install.
- Your directory is your gallery
- Supports JPG, PNG, HEIC, ARW, RAW images.
- Super fast preview and thumbnail generation using libvips

### Run prerequisites

**macOS:**
```bash
brew install vips
```

**Ubuntu/Debian:**
```bash
sudo apt-get install libvips-dev
```

**Fedora:**
```bash
sudo dnf install vips-devel
```

**Windows:**
Just run the .exe

### Build

```bash
go build -o directory-server
```

## Usage

### Command-Line Arguments

Run the server with command-line arguments:

```bash
./directory-server -root /path/to/your/directory -port 8080
```

**Arguments:**
- `-root`: Root directory to serve (default: current directory)
- `-port`: Port to listen on (default: 8080)

**Examples:**

```bash
# Use default settings (current directory, port 8080)
./directory-server

# Specify root directory
./directory-server -root /path/to/images

# Specify both root directory and port
./directory-server -root /path/to/images -port 3000
```

### Access the Web Interface

Open your browser and navigate to:
```
http://localhost:8080
```

## API Endpoints

- `GET /` - Main web interface
- `GET /api/list?path=/` - JSON API for directory listing
- `GET /api/thumbnail/<path>` - Serves thumbnail for an image
- `GET /static/<path>` - Serves static files

## Security

The server includes path traversal protection to ensure users can only access files within the specified root directory.

