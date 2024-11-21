# ISUCON14 Qualify
予選用リポジトリ

## 初期設定
```bash
# SSH後に実行
$ cd /tmp && \
  git clone https://github.com/melanmeg/isucon14-qualify.git
  # 適宜、Git管理 `init commit`を実施

$ mv /home/isucon /tmp/isucon.bk && \
  mv isucon14-qualify /home/isucon && \
  ssh-keygen -t ed25519 -C "" -f ~/.ssh/id_ed25519 -N "" && \
  sudo apt update -y

# private-isuでGOROOT空だったので、そのような場合にGoをインストールする
$ sudo rm -rf /usr/local/go
$ TAR_FILENAME=$(curl 'https://go.dev/dl/?mode=json' | jq -r '.[0].files[] | select(.os == "linux" and .arch == "amd64" and .kind == "archive") | .filename')
$ URL="https://go.dev/dl/$TAR_FILENAME"
$ curl -fsSL "$URL" -o /tmp/go.tar.gz && \
  sudo tar -C /usr/local -xzf /tmp/go.tar.gz && \
  rm -f /tmp/go.tar.gz
$ cat <<EOF >> ~/.bashrc
export GOROOT=/usr/local/go
export GOPATH=$HOME/go
EOF
```

## メモ
