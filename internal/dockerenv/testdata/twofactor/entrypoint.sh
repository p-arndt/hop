#!/bin/bash
# Configure and start four sshd instances, each demanding a different flavour of
# two-factor authentication. See the Dockerfile for what listens where.
set -euo pipefail

USER_NAME=deploy
USER_PASS=hunter2
# A fixed base32 TOTP secret, so the test can compute codes the server accepts.
# On a real host this comes out of `google-authenticator` and is never shared.
TOTP_SECRET=ZVUV2W2ZPPJXPKMKV4L2UAFPQY

id -u "$USER_NAME" >/dev/null 2>&1 || useradd -m -s /bin/bash "$USER_NAME"
echo "$USER_NAME:$USER_PASS" | chpasswd

# ---- the user's Google Authenticator state ----
#
# This is exactly the file `google-authenticator -t -d -f` writes: the secret on
# the first line, then its options. TOTP_AUTH is what selects time-based codes
# over counter-based ones. Rate limiting and one-time-use are deliberately left
# out: both are real protections, but they would make a test suite that dials
# several times in a row fail for reasons that have nothing to do with hop.
install -d -m 0700 -o "$USER_NAME" -g "$USER_NAME" "/home/$USER_NAME"
cat >"/home/$USER_NAME/.google_authenticator" <<EOF
$TOTP_SECRET
" TOTP_AUTH
" WINDOW_SIZE 3
EOF
chown "$USER_NAME:$USER_NAME" "/home/$USER_NAME/.google_authenticator"
chmod 0600 "/home/$USER_NAME/.google_authenticator"

# ---- the client key, for the publickey+code instance ----
install -d -m 0700 -o "$USER_NAME" -g "$USER_NAME" "/home/$USER_NAME/.ssh"
if [ -f /keys/id_ed25519.pub ]; then
  cp /keys/id_ed25519.pub "/home/$USER_NAME/.ssh/authorized_keys"
  chown "$USER_NAME:$USER_NAME" "/home/$USER_NAME/.ssh/authorized_keys"
  chmod 0600 "/home/$USER_NAME/.ssh/authorized_keys"
fi

# ---- host keys, privilege separation dir ----
ssh-keygen -A
mkdir -p /run/sshd

# ---- PAM stacks ----
#
# sshd has no option for picking a PAM service: portable OpenSSH uses its own
# program name (auth-pam.c defines SSHD_PAM_SERVICE as __progname). So the two
# stacks are selected by running two *copies* of the binary under the names the
# /etc/pam.d files are called, which is the only way to have one container serve
# more than one kind of authentication.
cp /usr/sbin/sshd /usr/sbin/sshd-totp
cp /usr/sbin/sshd /usr/sbin/sshd-password-totp

# The code alone. `auth required pam_google_authenticator.so` is the line every
# guide tells you to add; here it is the *only* auth line, so nothing but the
# verification code is asked for.
cat >/etc/pam.d/sshd-totp <<'EOF'
auth required pam_google_authenticator.so
account required pam_unix.so
session required pam_unix.so
EOF

# The unix password and then the code — two prompts, in two rounds, which is what
# a stack with both modules produces.
cat >/etc/pam.d/sshd-password-totp <<'EOF'
auth required pam_unix.so
auth required pam_google_authenticator.so
account required pam_unix.so
session required pam_unix.so
EOF

# ---- sshd configs ----
common() {
  cat <<EOF
Port $1
PidFile /run/sshd-$1.pid
UsePAM yes
KbdInteractiveAuthentication yes
PasswordAuthentication no
PermitRootLogin no
X11Forwarding no
PrintMotd no
Subsystem sftp /usr/lib/openssh/sftp-server
EOF
}

# 2222 — the code is the whole login.
{ common 2222
  echo "AuthenticationMethods keyboard-interactive"
  echo "PubkeyAuthentication no"
} >/etc/ssh/sshd_config.totp

# 2223 — the hardened setup: the key gets a *partial* success, then the server
# still requires the code. This is the case that proves hop carries on down its
# auth method list after a partial success instead of stopping at the key.
{ common 2223
  echo "AuthenticationMethods publickey,keyboard-interactive"
  echo "PubkeyAuthentication yes"
} >/etc/ssh/sshd_config.keytotp

# 2224 — password and code, both over keyboard-interactive, as two prompts.
{ common 2224
  echo "AuthenticationMethods keyboard-interactive"
  echo "PubkeyAuthentication no"
} >/etc/ssh/sshd_config.pwtotp

# 2225 — keyboard-interactive *and* password offered as alternatives (no
# AuthenticationMethods line, so either one on its own is enough). This is the
# only shape that shows what a client does after the user dismisses a prompt: an
# SSH client does not stop at the first method that fails, so without a sticky
# cancel one `esc` is answered by the next method asking the same person again.
#
# The password method here is only ever meant to be *offered*, never to succeed:
# it runs the code-only PAM stack, so whatever is sent as the password is checked
# against pam_google_authenticator. That is all the cancel test needs — a second
# method for a dismissed prompt to fall through to. Do not send the account
# password to this port expecting a login; use 2224 for that.
{ cat <<EOF
Port 2225
PidFile /run/sshd-2225.pid
UsePAM yes
KbdInteractiveAuthentication yes
PasswordAuthentication yes
PubkeyAuthentication no
PermitRootLogin no
PrintMotd no
EOF
} >/etc/ssh/sshd_config.both

/usr/sbin/sshd-totp -t -f /etc/ssh/sshd_config.totp
/usr/sbin/sshd-totp -t -f /etc/ssh/sshd_config.keytotp
/usr/sbin/sshd-totp -t -f /etc/ssh/sshd_config.both
/usr/sbin/sshd-password-totp -t -f /etc/ssh/sshd_config.pwtotp

echo "twofactor: secret $TOTP_SECRET user $USER_NAME"
/usr/sbin/sshd-totp -f /etc/ssh/sshd_config.totp
/usr/sbin/sshd-totp -f /etc/ssh/sshd_config.keytotp
/usr/sbin/sshd-totp -f /etc/ssh/sshd_config.both
/usr/sbin/sshd-password-totp -f /etc/ssh/sshd_config.pwtotp
echo "twofactor: listening on 2222 (code) 2223 (key+code) 2224 (password+code) 2225 (code or password)"

# Hold the container open, and let a `docker logs` show what the daemons say.
tail -f /var/log/auth.log 2>/dev/null || sleep infinity
