# Optional AI providers (embedding + rerank)

Search uses BM25 only by default. The two services below are optional and
start with the `ai` compose profile. They are plain TEI providers (OpenAI-style
APIs also work); enter their URLs in **Admin → Search settings**.

| Service  | Model                     | URL on the compose network |
|----------|---------------------------|----------------------------|
| `embed`  | `BAAI/bge-m3` (dim 1024)  | `http://embed:80`          |
| `rerank` | `BAAI/bge-reranker-v2-m3` | `http://rerank:80`         |

```sh
# amd64 (Linux, CI)
docker compose --profile ai up -d

# Apple Silicon (native arm64, no emulation)
TEI_TAG=cpu-arm64-1.9.4 TEI_PLATFORM=linux/arm64 docker compose --profile ai up -d
```

## Pinned images

Text Embeddings Inference publishes separate CPU tags per architecture:
`cpu-1.9.x` is amd64 only and `cpu-arm64-1.9.x` is arm64 only. The compose
file pins `cpu-1.9.4` (default) and `cpu-arm64-1.9.4`.

Both services run with `--max-batch-tokens 2048`. Without it, the bge-m3
warm-up (default batch of 16,384 tokens) is OOM-killed on a 7.6 GB Docker VM.

## Memory

Measured on Apple Silicon: `embed` uses about 4.6 GiB and `rerank` about
4.0 GiB. **Running both needs a Docker VM of roughly 10 GB**; with 7.65 GB
both are OOM-killed during warm-up. The first start downloads each model
(about 2.3 GB, into the `hf-cache` volume) and takes around five minutes.

With less memory, start only the embedding service
(`docker compose --profile ai up -d embed`) and enter only its URL. The
pipeline falls back to `bm25+vector` without a reranker.

## Low memory: Ollama (embedding only)

```sh
docker run -d --name ollama \
  --network "$(docker compose ps -q db | xargs docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}')" \
  -p 11434:11434 -v ollama:/root/.ollama ollama/ollama
docker exec ollama ollama pull bge-m3
```

In the UI, set the embedding URL to `http://ollama:11434/v1` (the container
must be on the compose network, as above; from the host use
`http://localhost:11434/v1`) and the model to `bge-m3`. Ollama serves
embeddings through its OpenAI-compatible `/v1` API but has **no reranker**,
so search runs BM25 + vector without reranking. Leave the rerank URL empty.
