FROM docker.m.daocloud.io/library/alpine:3.20
WORKDIR /seed
COPY mapping.yaml /seed/mapping.yaml
CMD ["sh", "-c", "mkdir -p /data; if [ ! -f /data/mapping.yaml ]; then cp /seed/mapping.yaml /data/mapping.yaml; echo '[seed] wrote default /data/mapping.yaml'; else echo '[seed] /data/mapping.yaml already exists, skip'; fi"]
