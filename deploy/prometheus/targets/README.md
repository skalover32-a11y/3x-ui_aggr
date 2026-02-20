# Custom Prometheus targets

Put JSON files with file_sd format into this folder.

Example `node_exporters.json`:

```json
[
  {
    "targets": ["10.0.0.10:9100", "10.0.0.11:9100"],
    "labels": {
      "job": "node_exporter",
      "env": "prod"
    }
  }
]
```

Then reload Prometheus config:

```bash
curl -X POST http://localhost:19090/-/reload
```
