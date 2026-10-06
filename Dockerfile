FROM python:3.13-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Install Go
RUN curl -fsSL https://go.dev/dl/go1.26.5.linux-amd64.tar.gz | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /app

# Copy repo
COPY . .

# Python deps
RUN python -m venv venv && \
    ./venv/bin/pip install --upgrade pip && \
    ./venv/bin/pip install cactus-needle==3.0.6 jax==0.4.38 jaxlib==0.4.38 flax==0.10.4

EXPOSE 8080

ENV KUBECONFIG=/app/kubeconfig.yaml

CMD ["./venv/bin/python3", "runtime/server.py", "8080"]
