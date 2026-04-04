# LLM Pool Proxy

A high-performance, concurrent API pool proxy service written in Go. This service enables users to aggregate multiple unreliable or rate-limited text generation API endpoints and present them as a single, highly available API to any application.

## Features

- **Concurrent Routing & Load Balancing**: Fast routing written in Go. Requests are balanced across available endpoints using Round-Robin, Random, or Weighted strategies.
- **Auto Health Checks & Failovers**: Automatically detects unreachable or error-prone endpoints. Bypasses failing endpoints in real time and retries requests using a different healthy provider.
- **Model Pools (Aliases)**: Group multiple differing models from varying providers into a single "Pool". You can configure your client application to target `Pool_1`, and the proxy will dynamically route to any backend model specified in that pool.
- **Web Admin Interface**: An elegant dashboard to view live endpoint statuses, manage your API keys, manually add or disable endpoints, and group models into pools.
- **Live Request Logging**: Real-time observability UI showing exactly what requests came in, which endpoint served them, and any resulting upstream errors.
- **Intelligent Error Parsing**: Intercepts error messages from upstream providers to provide detailed error context to the transparent Request Logs, allowing you to troubleshoot upstream limits easily.
- **Live Configuration Reload**: Configurations, aliases, and endpoint keys can be modified securely from the web interface without restarting the proxy.

## Setting Up

1. **Install Go**: Ensure you have Go 1.20+ installed to build the binary.
2. **Configure placeholders**: Copy `config.yaml` or edit the placeholder `config.yaml` to include your endpoints and API keys.
3. **Build and Run**:

```bash
go mod tidy
go build -o proxy
./proxy
```

4. **Access the Admin Panel**: Navigate to `http://localhost:5066/admin` in your web browser.

## Client Integration

Integrating into API clients is simple. Just set the custom API endpoint URL in your client to:

`http://localhost:5066/v1`

And use either an actual model name (if it's accessible directly on your backends) or a custom Pool name defined in your Pool Aliases.
