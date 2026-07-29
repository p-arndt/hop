# A real OpenSSH server with real Google Authenticator 2FA, for hop's end-to-end
# authentication test. Nothing here is a mock: it is Ubuntu's openssh-server and
# Ubuntu's libpam-google-authenticator, configured the way the guides tell you to
# configure them, so what the test exercises is what a user's hardened box does.
#
# It listens four times, because "2FA" means several different handshakes:
#
#   2222  keyboard-interactive only        — the code is the whole login
#   2223  publickey,keyboard-interactive   — key first, then the code (the hardened setup)
#   2224  password then code               — the unix password and the code, both over PAM
#   2225  keyboard-interactive OR password — two methods offered as alternatives
#
# The user's TOTP secret is fixed (see entrypoint.sh) rather than generated, so
# the test can compute valid codes for it. That is the one thing about this
# container that is not how you would set up a real host, and it is why it must
# only ever be run on loopback.
FROM ubuntu:24.04

RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      openssh-server \
      libpam-google-authenticator \
 && rm -rf /var/lib/apt/lists/*

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 2222 2223 2224 2225
CMD ["/entrypoint.sh"]
