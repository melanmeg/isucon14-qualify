# ISUCON14 Qualify
予選用リポジトリ

## 初期設定
- SSH後に実行
```bash
# デプロイキー設定
$ ssh-keygen -t ed25519 -C "" -f ~/.ssh/id_ed25519 -N "" && \
  sudo apt update -y

# 適宜、Git管理 `init commit`を実施
$ git clone https://github.com/melanmeg/GIT_REPO.git /tmp/GIT_REPO && \
  mv /home/isucon/webapp /home/isucon/webapp.bk && \
  mv /tmp/GIT_REPO/{*,.gitignore,.github,.git} /home/isucon/ && \
  rm -rf /tmp/GIT_REPO
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

## メモ
