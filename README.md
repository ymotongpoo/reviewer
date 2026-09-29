# reviewer

AI エージェントが書いた Markdown などの文書に、GitHub の PR レビューのような行コメントとプロジェクト全体へのコメントを付け、フィードバックをファイルとしてエージェントに返すためのツールです。仕様は [SPEC.md](SPEC.md) を参照してください。

## ビルド

Go 1.25 以降と Node.js が必要です。

```sh
make build        # web/ をビルドして bin/reviewer を作る
make dist         # dist/<os>-<arch>/reviewer にクロスコンパイル
make test
```

`make dist` の対象は、既定では linux-amd64、linux-arm64、darwin-arm64、darwin-amd64 です。`make dist DIST_TARGETS="linux-amd64"` のように絞り込めます。Linux 向けは静的リンクです。

Web UI はバイナリに埋め込まれるので、リモートマシンにはバイナリを1つ置けば動きます。

```sh
scp dist/linux-amd64/reviewer devbox:~/bin/
```

## 使い方

リモートマシンで起動し、表示された URL を手元のブラウザで開きます。mDNS（Bonjour / Avahi）が使える環境では、ホスト名でアクセスできます。

```sh
devbox$ reviewer serve ./docs
Serving /home/me/docs
  data: /home/me/docs/.reviewer
  Open: http://devbox.local:7777/?token=3f9a...
        http://192.168.1.10:7777/?token=3f9a...
```

既定では全インターフェースの IPv4 と IPv6 の両方で待ち受けます（`--bind all`）。`.local` の名前が IPv6 のアドレスに解決される環境でもつながります。

このマシンからだけ開けるようにしたいときは `--bind 127.0.0.1` を指定します。

1. 左のツリーからファイルを開き、行番号をクリックしてコメントします。Shift+クリックかドラッグで範囲を選べます。入力内容は自動で下書き保存されます。
2. Markdown ファイルでは、ファイルヘッダーの「プレビュー」を押すと右側にレンダリング結果を並べて表示します。スクロールはソースに合わせて追従し、オンオフの状態はブラウザに保存されます。
   - frontmatter に `emoji` と `type` / `topics` があるファイル（Zenn の記事）と `books/` 以下のファイルは、[zenn-editor](https://github.com/zenn-dev/zenn-editor) の `zenn-markdown-html` でレンダリングします。`:::message`、`:::details`、ファイル名付きコードブロック、数式、脚注などの Zenn 記法に対応しています。それ以外のファイルは GFM としてレンダリングします。どちらを使うかはプレビュー上部のメニューで切り替えられます。
   - 数式は同梱の KaTeX で描画します。YouTube やツイートなどの埋め込みは読み込まず、リンクだけを表示します。
3. 「± 修正案を挿入」で `suggestion` ブロックを挿入すると、選択範囲の置き換え案として扱われます。
4. プロジェクト全体へのコメントは「概要・全体コメント」に、ファイル全体へのコメントはファイルヘッダーの「ファイルにコメント」から書きます。
5. 「レビューを提出」を押すと、`.reviewer/rounds/<N>/feedback.md` と `feedback.json` が書き出され、エージェント用の指示文がコピーされます。これをエージェントに貼り付けます。
6. エージェントがファイルを直して `response.json` を書くと、UI は自動で更新されます。エージェントの返答は各スレッドに表示されます。ファイル表示は常に現在の内容です。そのラウンドで何が直されたかは、概要ページのラウンド履歴にある「差分を見る」で確認できます。差分ページには、そのラウンドのコメントが提出時点の行位置に表示されます。解決済みにしたコメントはファイル表示から消え、差分ページでだけ見られます。
7. 納得したコメントは「解決」に、そうでないものには「返信」します。コメントや返信を書くと次のラウンドが自動で始まり、解決していないコメントは次の提出に持ち越されます。

### エージェントからの返答形式

```json
{
  "round": 1,
  "responses": [
    { "id": "C-1", "status": "addressed", "message": "削除しました" },
    { "id": "C-2", "status": "wontfix", "message": "理由…" },
    { "id": "C-3", "status": "question", "message": "質問…" }
  ],
  "summary": "変更の概要"
}
```

形式は `feedback.md` の末尾にも書かれているので、エージェントには指示文を渡すだけで足ります。

### その他のコマンド

```sh
reviewer status [DIR]                         # ラウンドと未解決コメントの件数
reviewer export [DIR] [--round N] [--format md|json]
```

## エージェントへの直接送信（Hermes Agent）

同じマシンで [Hermes Agent](https://github.com/NousResearch/hermes-agent) の gateway が動いていれば、提出したフィードバックを reviewer から直接 Hermes のセッションに送れます。送り先は、文書を書いたときのセッション（Discord のスレッドなど）です。

### Hermes 側の準備（1回だけ）

`~/.hermes/.env` に次を追記して、gateway を再起動します。

```sh
API_SERVER_ENABLED=true
API_SERVER_KEY=<openssl rand -hex 32 で作った値>   # 16文字以上。短いと gateway が起動を拒否する
```

```sh
hermes gateway restart   # PATH に無ければ ~/hermes/hermes-agent/venv/bin/hermes
curl -s localhost:8642/health
```

API サーバーは既定で `127.0.0.1:8642` で待ち受けます。reviewer が同じマシンにあるので、LAN に公開する必要はありません。reviewer は `~/.hermes/.env` から URL とキーを自動で読むので、reviewer 側の設定は要りません。

### 使い方

1. reviewer を起動すると、ヘッダーに「🤖 Hermes Agent: 送信先を選ぶ」と表示されます。クリックして、文書を書いたセッションを選びます。Discord のセッションが新しい順に先頭に並びます。
2. 「レビューを提出」のダイアログで「Hermes Agent に送信する」にチェックを入れたまま提出します。
3. 概要ページの「🤖 Hermes Agent」パネルに、エージェントの返答と作業ログがリアルタイムに流れます。
   - 承認が必要な操作では、承認カードが出ます。「今回だけ許可」「拒否」などで答えます。
   - 途中で止めたいときは「停止」を押します。
   - 送り直したいときは「再送信」を押します。
4. 開始時と完了時には、そのセッションの Discord スレッドに短い通知が投稿されます。Hermes の `hermes mcp serve` を経由して、gateway のボットが投稿します。
5. コメントごとの返答は、今までどおりエージェントが書く `response.json` から取り込みます。

指示文をコピーして手で渡す方法も、そのまま使えます。

### 設定

```toml
[agent]
kind = "auto"          # auto（Hermes が設定済みなら使う）| hermes | none
auto_send = true       # 提出ダイアログの「送信する」の初期値
notify = "hermes"      # hermes（セッションのスレッドに投稿）| discord-webhook | none

[agent.hermes]
url = ""               # 空なら ~/.hermes/.env から自動検出
api_key_env = "HERMES_API_KEY"
profile = ""           # 指定すると /p/<profile>/ を使う
command = ""           # hermes CLI のパス。空なら PATH と ~/hermes/hermes-agent/venv/bin を探す

[agent.discord_webhook]
url = ""               # notify = "discord-webhook" のときに使う
```

### 注意

- reviewer は既定で LAN から開けます。トークン付きの URL を知っている人は、エージェントに作業を依頼できます。
- Hermes の API キーはサーバー側だけで使い、ブラウザには渡しません。
- API から送ったターンでは、危険と判定されたコマンドが Hermes の `approvals.unattended_mode`（既定は `deny`）によって自動的に拒否されます。reviewer の承認カードで答えられるのは、Hermes が承認を求めてきた操作だけです。

### Hermes なしで試す

```sh
go build -o bin/fakehermes ./cmd/fakehermes
HERMES_HOME=/tmp/fh ./bin/fakehermes &          # 偽の API を 127.0.0.1:8642 で起動し、/tmp/fh/.env を書く
HERMES_HOME=/tmp/fh ./bin/reviewer serve ./docs
```

偽の Hermes は、途中で承認を1回求めます。その後、すべてのコメントに「対応済み」と返す `response.json` を書きます。

## 設定

`~/.config/reviewer/config.toml`（グローバル）と `DIR/.reviewer/config.toml`（プロジェクト）を読み込みます。プロジェクトの設定が優先されます。

```toml
data_dir = ".reviewer"      # "xdg" にすると ~/.local/share/reviewer/<project>-<hash> に保存する
port = 7777
bind = "all"                # 既定。"127.0.0.1" にするとこのマシンからのみ
exclude = ["drafts/", "*.bak"]
prompt_template = "{{.FeedbackPath}} を読んで対応し、{{.ResponsePath}} に返答してください。"

[[labels]]
name = "must"
description = "必須"

[anchor]
fuzzy_threshold = 0.7
```

`prompt_template` では `.Round` `.FeedbackPath` `.FeedbackJSONPath` `.ResponsePath` `.Project` を使えます。

## セキュリティ

- 既定では全インターフェースで待ち受けるので、同じネットワークの他のマシンから届きます。通信は HTTP の平文なので、信頼できる LAN か Tailscale などの内側で使ってください。このマシンからだけ開けるようにするには `--bind 127.0.0.1` を指定します。起動のたびにランダムなトークンを発行し、トークン付き URL で認証します。固定したい場合は `--token` か `REVIEWER_TOKEN` を使います。
- 変更系の API には `X-Reviewer` ヘッダーを必須にしています（CSRF 対策）。
- 対象ディレクトリの外にあるパス、`.git/`、データディレクトリは読めないようにしています。

## 開発

```sh
reviewer serve ./sample --token dev     # API サーバー
cd web && npm run dev                   # Vite の開発サーバー（/api は 7777 番に中継する）
```
