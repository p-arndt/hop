# A real OpenSSH server with three accounts on three different login shells, for
# hop's working-directory tracking test.
#
# What it exists to prove is that hop's shell integration works against the real
# thing: the prompt hook is typed into a real bash and a real zsh, those shells
# really emit OSC 7 before every prompt, and a real `cd` really moves what they
# report. Nothing here is a mock — it is Ubuntu's openssh-server, Ubuntu's shells,
# and their stock configuration.
#
#   bashy  /bin/bash      — the hook installs
#   zshy   /usr/bin/zsh   — the hook installs
#   fishy  /usr/bin/fish  — the hook must NOT be installed, and the cwd stays unknown
#
# One sshd, on 2222, public key only. The accounts share the client key the test
# generates (mounted at /keys), and the container only ever runs on loopback.
FROM ubuntu:24.04

RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      openssh-server \
      zsh \
      fish \
 && rm -rf /var/lib/apt/lists/*

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 2222
CMD ["/entrypoint.sh"]
