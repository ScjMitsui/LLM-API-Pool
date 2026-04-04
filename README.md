<div align="center">
  <h1>🌊 LLM-API-Pool</h1>
  <p><strong>A high-performance, concurrent API pool proxy service for LLMs</strong></p>
</div>

<p align="center">
  <img src="https://img.shields.io/badge/Made%20with-Go-1f425f.svg" alt="Made with Go">
  <img src="https://img.shields.io/badge/Concurrency-High-brightgreen.svg" alt="High Concurrency">
  <img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License">
</p>

---

**LLM-API-Pool** is a lightning-fast, highly concurrent API pool proxy service written in Go. It allows you to seamlessly aggregate multiple unreliable or rate-limited text generation API endpoints and present them as a single, bulletproof API to any client application.

## ✨ Key Features

- 🏎️ **Concurrent Routing & Load Balancing**  
  Lightning-fast routing written in Go. Balance requests across your available endpoints using Round-Robin, Random, or Weighted strategies.
  
- 🛡️ **Auto Health Checks & Failover**  
  Say goodbye to downtime. The proxy automatically detects unreachable or error-prone endpoints, bypassing them in real time and intelligently retrying requests on healthy providers.
  
- 🏊 **Model Pools (Aliases)**  
  Group different models from various providers into a single logical "Pool". Configure your client to target a generic name like `My_Pool`, and let the proxy dynamically route your prompt to any backend model within that pool.

- 🎛️ **Web Admin Interface**  
  An elegant, built-in dashboard to monitor live endpoint statuses, manage API keys, toggle endpoints, and organize your model pools with ease.

- 📊 **Live Request Logging**  
  Real-time observability. See exactly what requests are coming in, which endpoint is serving them, and monitor any errors directly from the UI.
  
- 🧠 **Intelligent Error Parsing**  
  Intercepts and decodes raw error messages from upstream providers. It feeds detailed, human-readable context back into your request logs to simplify troubleshooting rate limits or authentication outages.

- 🔄 **Live Configuration Reloads**  
  Tweak configurations, update aliases, or change pool settings securely from the web interface on the fly—zero downtime or restarts required.

## 🚀 Getting Started

### 1. Prerequisites
Ensure you have [Go 1.20+](https://go.dev/dl/) installed to build the binary.

### 2. Configuration
Copy the provided `config.yaml` template and populate it with your specific endpoints and API keys.

### 3. Build & Run
Get the proxy up and running in seconds:

```bash
go mod tidy
go build -o proxy
./proxy
```

### 4. Access the Dashboard
Navigate to your web browser and open:
[http://localhost:5066/admin](http://localhost:5066/admin)

## 🔌 Client Integration

Integrating into API clients (like SillyTavern, UI clients, or your own code) is effortless. 

Simply set the custom API endpoint URL in your client to:  
`http://localhost:5066/v1`

For the model name, either use an exact model name available directly on your backends, or use a custom **Pool Name** defined in your Pool Aliases.

---
<div align="center">
  <i>Supercharge your LLM capabilities with limitless availability. Built with ❤️ in Go.</i>
</div>
