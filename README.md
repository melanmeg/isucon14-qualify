# ISUCON14 Qualify
予選用リポジトリ

## 初期設定
- SSH後に実行
```bash
# デプロイキー設定
$ ssh-keygen -t ed25519 -C "" -f ~/.ssh/id_ed25519 -N "" && \
  sudo apt update -y

# 適宜、Git管理 `init commit`を実施
$ git clone https://github.com/melanmeg/isucon14-qualify.git /tmp/isucon14-qualify && \
  mv /home/isucon/webapp /home/isucon/webapp.bk && \
  mv /tmp/isucon14-qualify/{*,.gitignore,.github,.git} /home/isucon/ && \
  rm -rf /tmp/isucon14-qualify
$ cd /home/isucon && git add -A && git commit -m "init commit" && git push

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
export PATH=/usr/local/go/bin:$PATH
EOF
```

## 各人サーバー割り当て

1. melanmeg, 52.198.45.15, 192.168.0.11
2. megumish, 18.180.233.35, 192.168.0.12
3. nwiizo, 54.250.48.240, 192.168.0.13

- 計測サーバー, 18.178.0.235, 192.168.0.179

- test
