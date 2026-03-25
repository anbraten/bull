FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    openssh-server \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir /var/run/sshd \
    && sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config \
    && sed -i 's/#PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config

# docker shim: echo all commands instead of running real docker
RUN printf '#!/bin/sh\necho "[docker] $*"\n' > /usr/local/bin/docker \
    && chmod +x /usr/local/bin/docker

# docker compose shim (standalone binary variant)
RUN ln -s /usr/local/bin/docker /usr/local/bin/docker-compose

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D"]
