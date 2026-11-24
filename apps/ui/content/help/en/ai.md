By default, search matches keywords (BM25). You can add semantic search (embeddings) and reranking with two optional services.

## How to turn it on

1. Start the optional services with the compose profile `ai`.
2. Under **Admin → Search**, enter the embedding and rerank addresses:
   - Deployment (compose network): `http://embed` and `http://rerank`.
   - Dev: `http://embed:80` and `http://rerank:80`.
3. Click **Test connection**, then **Save**.
4. Click **Recompute vectors** to embed existing content. The progress bar shows how many contributions have a vector.

> [!NOTE]
> Running both services needs a Docker VM with about 10 GB of RAM. On arm64 (Mac) use the arm64 image tags. Details in `docker/embed/README.md`.

## When a service does not answer

Search keeps working: it falls back to keywords only and says it runs in degraded mode.
