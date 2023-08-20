FROM bash
RUN apk --no-cache add jq
COPY /download.sh /
ENTRYPOINT /usr/local/bin/bash /download.sh
