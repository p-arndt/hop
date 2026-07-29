#!/bin/bash
# Create one account per login shell, authorize the mounted client key on each,
# and start a single sshd. See the Dockerfile for what each account is for.
set -euo pipefail

# A directory whose name has a space in it, so the test can prove the path
# survives the round trip through an escape sequence and back.
SPACE_DIR="/srv/hop test dir"
install -d -m 0755 "$SPACE_DIR"

add_user() {
  local name=$1 shell=$2
  id -u "$name" >/dev/null 2>&1 || useradd -m -s "$shell" "$name"
  install -d -m 0700 -o "$name" -g "$name" "/home/$name/.ssh"
  if [ -f /keys/id_ed25519.pub ]; then
    cp /keys/id_ed25519.pub "/home/$name/.ssh/authorized_keys"
    chown "$name:$name" "/home/$name/.ssh/authorized_keys"
    chmod 0600 "/home/$name/.ssh/authorized_keys"
  fi
}

add_user bashy /bin/bash
add_user zshy /usr/bin/zsh
add_user fishy /usr/bin/fish

# zsh with no .zshrc runs its first-run configuration wizard on an interactive
# login, which takes the screen and never reaches a prompt. An empty rc-file is
# what tells it the user has already been asked.
touch /home/zshy/.zshrc
chown zshy:zshy /home/zshy/.zshrc

ssh-keygen -A
mkdir -p /run/sshd

cat >/etc/ssh/sshd_config.shellhost <<'EOF'
Port 2222
PidFile /run/sshd-2222.pid
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
X11Forwarding no
PrintMotd no
UsePAM yes
Subsystem sftp /usr/lib/openssh/sftp-server
EOF

/usr/sbin/sshd -t -f /etc/ssh/sshd_config.shellhost
echo "shellhost: accounts bashy (bash) zshy (zsh) fishy (fish)"
/usr/sbin/sshd -f /etc/ssh/sshd_config.shellhost
echo "shellhost: listening on 2222"

# Hold the container open, and let a `docker logs` show what sshd says.
tail -f /var/log/auth.log 2>/dev/null || sleep infinity
