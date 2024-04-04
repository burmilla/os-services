FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y tar wget && \
    rm -rf /var/lib/apt/lists/*
RUN cd /usr/local/bin && \
    wget https://github.com/osrg/gobgp/releases/download/v3.25.0/gobgp_3.25.0_linux_amd64.tar.gz -O gobgp.tar.gz && \
    tar zxvf gobgp.tar.gz && \
    rm gobgp.tar.gz
COPY /start.sh /
ENTRYPOINT ["/start.sh"]
