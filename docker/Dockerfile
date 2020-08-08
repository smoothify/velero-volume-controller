# Production stage
FROM alpine:3.10

# Add the go binary already built
ADD release/linux/amd64/velero-volume-controller /bin/

# copy the config files from the current working dir
COPY examples /velero-volume-controller/examples

ENTRYPOINT ["/bin/velero-volume-controller", "-c", "/velero-volume-controller/examples/config.yml", "-v", "4", "-logtostderr", "true"]
