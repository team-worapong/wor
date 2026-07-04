// Package servicefiles generates the starter source tree for a new
// service. As of the 2026-07-04 template redesign (see
// docs/services.md) there are five templates: static (public/ only),
// node (Node.js, PM2), go (Go, systemd on Linux / PM2 elsewhere),
// python (Python, systemd on Linux / PM2 elsewhere), and php (PHP-FPM,
// assumed already running). Every process-supervised template (node,
// go, python) gets a functionally equivalent starter app: a `/health`
// endpoint, JSON `/api/*` responses, and static file serving from
// `public/` -- so switching templates for a new service is a language
// choice, not a feature tradeoff.
package servicefiles

import (
	"fmt"
	"os"
	"path/filepath"

	"wor/internal/domainmodel"
)

func writeIfAbsent(path string, content []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func nodeAppSource(service string, port int) string {
	return fmt.Sprintf(`const http = require('http');
const fs = require('fs');
const path = require('path');

const port = process.env.PORT || %d;
const service = %q;
const publicDir = path.join(__dirname, 'public');

const mime = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon'
};

function json(res, data) {
  res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
  res.end(JSON.stringify(data));
}

function sendFile(res, file) {
  fs.readFile(file, (err, data) => {
    if (err) {
      res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' });
      res.end('Not Found');
      return;
    }
    res.writeHead(200, { 'Content-Type': mime[path.extname(file)] || 'application/octet-stream' });
    res.end(data);
  });
}

const server = http.createServer((req, res) => {
  const urlPath = decodeURIComponent((req.url || '/').split('?')[0]);
  if (urlPath === '/health') return json(res, { ok: true, service });
  if (urlPath.startsWith('/api/')) return json(res, { service, path: urlPath });

  if (fs.existsSync(publicDir)) {
    const safePath = path.normalize(urlPath).replace(/^\.\.(\/|\\|$)/, '');
    let file = path.join(publicDir, safePath === '/' ? 'index.html' : safePath);
    if (!file.startsWith(publicDir)) {
      res.writeHead(403);
      res.end('Forbidden');
      return;
    }
    fs.stat(file, (err, stat) => {
      if (!err && stat.isDirectory()) file = path.join(file, 'index.html');
      sendFile(res, file);
    });
    return;
  }

  json(res, { service, message: 'WOR Node service ready' });
});

server.listen(port, () => {
  console.log(service + ' listening on port ' + port);
});
`, port, service)
}

func packageJSON(service string) string {
	return fmt.Sprintf(`{
  "name": %q,
  "version": "1.0.0",
  "private": true,
  "scripts": {
    "dev": "node --watch app.js",
    "start": "node app.js"
  }
}
`, service)
}

const nodeGitignore = "node_modules/\n.env\n*.log\nlogs/\ndist/\nbuild/\ncoverage/\n.cache/\n.tmp/\n"

// goModule returns a placeholder module path. It intentionally does
// not attempt to guess a real import path (github.com/... etc.) -- the
// user can run `go mod edit -module=...` later; wor only needs the
// module to exist so `go build` works out of the box.
func goModFile(service string) string {
	return fmt.Sprintf("module %s\n\ngo 1.21\n", service)
}

func goMainSource(service string, port int) string {
	return fmt.Sprintf(`package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
)

func main() {
	port := %d
	if v := os.Getenv("PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			port = p
		}
	}
	service := %q
	publicDir := "public"

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "service": service})
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"service": service, "path": r.URL.Path})
	})
	if info, err := os.Stat(publicDir); err == nil && info.IsDir() {
		mux.Handle("/", http.FileServer(http.Dir(publicDir)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"service": service, "message": "WOR Go service ready"})
		})
	}

	addr := ":" + strconv.Itoa(port)
	log.Printf("%%s listening on %%s", service, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
`, port, service)
}

const goGitignore = "/app\n/app.exe\n.env\n*.log\nlogs/\n"

func pythonAppSource(service string, port int) string {
	return fmt.Sprintf(`import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SERVICE = %q
PUBLIC_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "public")


class Handler(BaseHTTPRequestHandler):
    def _json(self, data, status=200):
        body = json.dumps(data).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/health":
            return self._json({"ok": True, "service": SERVICE})
        if path.startswith("/api/"):
            return self._json({"service": SERVICE, "path": path})

        file_path = os.path.join(PUBLIC_DIR, "index.html" if path == "/" else path.lstrip("/"))
        if os.path.isdir(PUBLIC_DIR) and os.path.commonpath([os.path.abspath(file_path), PUBLIC_DIR]) == PUBLIC_DIR:
            if os.path.isfile(file_path):
                with open(file_path, "rb") as f:
                    body = f.read()
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return
        self._json({"service": SERVICE, "message": "WOR Python service ready"}, status=404 if path != "/" else 200)


def main():
    port = int(os.environ.get("PORT", %d))
    server = ThreadingHTTPServer(("0.0.0.0", port), Handler)
    print(f"{SERVICE} listening on :{port}")
    server.serve_forever()


if __name__ == "__main__":
    main()
`, service, port)
}

const pythonGitignore = "__pycache__/\n*.pyc\n.venv/\n.env\n*.log\nlogs/\n"
const genericGitignore = ".DS_Store\nThumbs.db\n"

func publicHTML(title, service string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <link rel="stylesheet" href="/assets/css/app.css">
</head>
<body>
  <main>
    <h1>%s</h1>
    <p>Service: %s</p>
  </main>
  <script src="/assets/js/app.js"></script>
</body>
</html>
`, title, title, service)
}

func exampleHTML(title string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s Example</title>
  <link rel="stylesheet" href="/assets/css/app.css">
</head>
<body>
  <main>
    <h1>Example Page</h1>
    <p>This file lives in public/example.html.</p>
  </main>
</body>
</html>
`, title)
}

const appCSS = "body { font-family: system-ui, sans-serif; margin: 40px; }\nmain { max-width: 720px; }\n"
const appJS = "console.log('WOR service ready');\n"

func createPublicHTML(dir, service, title string) error {
	if err := writeIfAbsent(filepath.Join(dir, "index.html"), []byte(publicHTML(title, service))); err != nil {
		return err
	}
	if err := writeIfAbsent(filepath.Join(dir, "example.html"), []byte(exampleHTML(title))); err != nil {
		return err
	}
	if err := writeIfAbsent(filepath.Join(dir, "assets", "css", "app.css"), []byte(appCSS)); err != nil {
		return err
	}
	return writeIfAbsent(filepath.Join(dir, "assets", "js", "app.js"), []byte(appJS))
}

func phpIndex(service string) string {
	return fmt.Sprintf(`<?php
header('Content-Type: text/html; charset=utf-8');
$service = %q;
?>
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>WOR PHP <?php echo htmlspecialchars($service, ENT_QUOTES, 'UTF-8'); ?></title>
</head>
<body>
  <main>
    <h1>WOR PHP Service Ready</h1>
    <p>Service: <?php echo htmlspecialchars($service, ENT_QUOTES, 'UTF-8'); ?></p>
  </main>
</body>
</html>
`, service)
}

// Create writes the starter source tree for a new service into dir,
// keyed off the five supported templates. port is only meaningful for
// process-supervised templates (node, go, python); template must
// already be validated/normalized.
func Create(dir, service string, port int, template string) error {
	if !domainmodel.IsValidTemplate(template) {
		return fmt.Errorf("unknown template: %s", template)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	switch template {
	case "static":
		if err := createPublicHTML(filepath.Join(dir, "public"), service, "WOR Static Site Ready"); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, ".gitignore"), []byte(genericGitignore)); err != nil {
			return err
		}
	case "node":
		if err := createPublicHTML(filepath.Join(dir, "public"), service, "WOR Node Service Ready"); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, "app.js"), []byte(nodeAppSource(service, port))); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, "package.json"), []byte(packageJSON(service))); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, ".gitignore"), []byte(nodeGitignore)); err != nil {
			return err
		}
	case "go":
		if err := createPublicHTML(filepath.Join(dir, "public"), service, "WOR Go Service Ready"); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, "go.mod"), []byte(goModFile(service))); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, "main.go"), []byte(goMainSource(service, port))); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, ".gitignore"), []byte(goGitignore)); err != nil {
			return err
		}
	case "python":
		if err := createPublicHTML(filepath.Join(dir, "public"), service, "WOR Python Service Ready"); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, "app.py"), []byte(pythonAppSource(service, port))); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, "requirements.txt"), []byte("")); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, ".gitignore"), []byte(pythonGitignore)); err != nil {
			return err
		}
	case "php":
		if err := createPublicHTML(filepath.Join(dir, "public"), service, "WOR PHP Site Ready"); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, "public", "index.php"), []byte(phpIndex(service))); err != nil {
			return err
		}
		if err := writeIfAbsent(filepath.Join(dir, ".gitignore"), []byte(genericGitignore)); err != nil {
			return err
		}
	}

	return nil
}
