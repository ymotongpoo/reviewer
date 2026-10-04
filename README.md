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

### サーバーを起動する

```sh
devbox$ reviewer serve            # ディレクトリの選択画面から始める
devbox$ reviewer serve ./docs     # ./docs を開いた状態で始める
reviewer dev
  Open: http://devbox.local:7777/?token=3f9a...
        http://192.168.1.10:7777/?token=3f9a...
```

表示された URL を手元のブラウザで開きます。mDNS（Bonjour / Avahi）が使える環境では、ホスト名でアクセスできます。既定では全インターフェースの IPv4 と IPv6 の両方で待ち受けます（`--bind all`）。このマシンからだけ開けるようにしたいときは `--bind 127.0.0.1` を指定します。

トークンは初回起動時に `~/.local/state/reviewer/token` に保存され、再起動しても変わりません。URL は `reviewer url` でいつでも表示できます。

### 常駐させる（systemd）

```sh
reviewer service install        # ~/.config/systemd/user/reviewer.service を作って起動する
reviewer service status | restart | uninstall
reviewer url                    # アクセス用の URL を表示する
```

ログアウト後やマシンの再起動後も動かし続けるには、`sudo loginctl enable-linger $USER` が必要です。未設定の場合は install のときに案内が出ます。

### ディレクトリを開いて切り替える

トップページ（`/`）でレビューするディレクトリを選びます。

- **最近開いたディレクトリ**：ラウンドの状況と、Hermes の送信先を一緒に表示します。
- **パス入力**：`~/` から始められ、Tab で補完できます。
- **ブラウズ**：ディレクトリをたどって選べます。

開いたディレクトリは `/p/<id>/` で表示されます。ブラウザのタブを分ければ、複数のディレクトリを同時に開けます。ヘッダーのディレクトリ名から、最近のディレクトリに切り替えることもできます。

ラウンドの履歴、コメント、Hermes の送信先は、そのディレクトリの `.reviewer/` に保存されます。サーバーを入れ直しても、同じディレクトリを開けばそのまま続きから使えます。サーバーの外に持つのは、最近開いたディレクトリの一覧（`~/.local/state/reviewer/projects.json`）とトークンだけです。

開けるディレクトリは、既定ではホームディレクトリの中に限られます。変更するには、グローバル設定に `roots = ["~/repos", "~/writing"]` のように書きます。

### レビューの流れ

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

## モバイル（Android Chrome）で使う

モバイル対応は未完了（実機ゲート未実施）です。[実機ゲート記録](docs/mobile/phase0-device-report.md)の項目は未検証で、自動テストは実機の代わりになりません。最終受け入れ E2E と実装後の性能評価も未実施です。

### 接続と公開 URL

Android からの接続には、Tailscale の MagicDNS と HTTPS を推奨します。同じ tailnet にサーバーと Android を接続し、[Tailscale Serve](https://tailscale.com/docs/reference/tailscale-cli/serve) でローカルの reviewer を公開する構成です。次のコマンドは設定例です。この実装作業では実行していません。

```sh
# サーバー側の例。Serve は別のターミナルで実行します。
reviewer serve --bind 127.0.0.1 --port 7777
tailscale serve --bg http://127.0.0.1:7777
```

Serve が表示する HTTPS URL を、グローバル設定の `public_url` に指定して reviewer を起動します。以下のホスト名は例なので、実際の URL に置き換えてください。

```toml
# ~/.config/reviewer/config.toml（[agent] などのテーブルより前に記述）
bind = "127.0.0.1"
port = 7777
public_url = "https://devbox.example.ts.net"
```

`public_url` には、トークン、クエリ、フラグメント、`/p/<id>/` を含めず、サーバーのルート URL を指定します。末尾の `/` は除かれます。この設定は表示と通知に使う URL を指定するもので、HTTPS の設定や待ち受けアドレスの変更は行いません。

```sh
reviewer serve --public-url https://devbox.example.ts.net
reviewer url
```

`--public-url` は設定ファイルより優先します。`--public-url=` で、その起動だけ公開 URL の設定を無効にできます。起動表示と `reviewer url` は公開 URL を先頭に出し、従来の直接接続用 URL も続けて表示します。`reviewer url` はグローバル設定を読むため、起動時だけ渡した `--public-url` や別の `--config` の値は反映しません。両方の表示をそろえるにはグローバル設定に記述してください。未設定時の URL の順序と通知先は従来どおりです。

Android Chrome で、先頭に表示されたトークン付き URL を開いて認証します。通知のプロジェクト URL は公開 URL を基点に生成され、トークンを含みません。

| 接続項目 | 注意と確認状況 |
|---|---|
| Tailscale の MagicDNS と HTTPS | Android からの到達性と、この経路での SSE は未検証（実機ゲート未実施）。D-NET-04、05 が対象です。 |
| Discord などの通知リンク | Cookie は [SameSite=Strict](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie#samesitesamesite-value) のため、別サイトから開いた最初の表示が 401 になることがあります。そのページの URL をもう一度開くか、Chrome のアドレスバーから直接開いてください。[S-08](docs/mobile/phase0-spike-report.md#s-08-別サイトからのcookie) では別サイトからの遷移が 401、続く直接アクセスが 200 でした。Discord 内ブラウザーと Android Chrome での結果は未検証（実機ゲート未実施、D-NET-01）。 |
| オリジンと認証 | ホスト名、HTTP か HTTPS か、ポートを変えたら、新しい入口のトークン付き URL を開き直してください。Cookie が引き継がれない場合があります。Cookie 自体はオリジン単位の保存ではありませんが、ホスト名が変われば別の Cookie になり、reviewer の待ち受けポートを変えると Cookie 名も変わります。Chrome 再起動後の保持を含め、未検証（実機ゲート未実施、D-NET-05、06）。 |
| トークン付き URL | 認証情報なのでチャットに貼らないでください。認証後は URL からトークンを除くリダイレクトを行いますが、履歴や共有先に残らない保証はありません。実機でどこに残るかは未検証（実機ゲート未実施、D-NET-07）。 |
| mDNS と LAN | Android の `.local` の名前解決は環境によって不安定なため、標準の入口には推奨しません。mDNS と LAN の IP による HTTP 接続は未検証（実機ゲート未実施、D-NET-02、03）。 |

### タッチ操作

| 操作 | 手順 | 実機の確認状況 |
|---|---|---|
| ファイルと補助操作 | 狭い画面では ☰ でファイル一覧を開き、ファイルをタップします。設定や AI 確認などはヘッダーの ⋯ から開きます。 | 未検証（実機ゲート未実施）。 |
| 行範囲コメント | 開始行の行番号をタップし、終了行をタップして、画面下の選択バーで「コメント」を押します。1 行なら最初のタップ後に「この行にコメント」を押します。「解除」で選択を外せます。 | 未検証（実機ゲート未実施）。 |
| 行の編集 | 行の ✎、または 1 行を選んだときの「この行を編集」を押します。本文のダブルタップでは編集を開始しません。変更した行も ✎ から編集できますが、未保存の変更行へのコメントはできません。 | Gboard の入力と確定後の操作は未検証（実機ゲート未実施、D-IME）。 |
| 文字範囲コメント | 本文を長押しして選択し、ハンドルで範囲を調整して、画面下の選択バーで「コメント」を押します。ファイルが更新されると選択は解除されます。 | Android 標準の選択メニューとの重なりを含め、未検証（実機ゲート未実施、D-SEL）。 |
| プレビュー | 幅 600px 未満では「プレビュー」を有効にすると「ソース」「プレビュー」のタブが表示されます。600px 以上では左右分割です。 | 回転時を含め、未検証（実機ゲート未実施）。 |

### 下書きと既知の制限

コメントの保存に失敗したら、コメント欄の「再試行」「端末に保存して閉じる」「編集を続ける」から選びます。保存結果が不明なときは自動で再送しません。編集で「サーバーを確認」が出た場合は、その操作で内容を確認してください。提出前にはコメントの保存を待ちますが、返信は「下書き保存」を押す必要があります。キーボード表示中に各ボタンを押せるかは未検証（実機ゲート未実施、D-CMP）。

- PWA、Service Worker、Web Push は未対応です。オフラインでのレビューや、オフライン中の変更を復帰後に自動送信する機能も未対応です。
- 端末に保存したコメントや編集の下書きは、その端末とブラウザーの同じオリジンでだけ復元できます。別端末や別の入口には移りません。ブラウザーのサイトデータを消すと失われます。Android のプロセス終了後の保持は未検証（実機ゲート未実施、D-LC-04）。
- IndexedDB を利用できない場合はメモリーに保存し、端末への保存が使えない旨を表示します。この場合、再読み込み後には下書きを復元できません。
- Gboard の日本語フリック入力、ローマ字入力、再変換、音声入力、変換中の別行移動や Undo は未検証（実機ゲート未実施、D-IME）。現在は行単位の編集を維持し、限定ブロック編集は導入していません。
- 縦横の切り替え、safe-area、画面分割、候補欄、フローティングキーボード、文字拡大は未検証（実機ゲート未実施、D-VP）。
- バックグラウンドからの復帰、画面ロック解除、Wi-Fi とモバイル回線の切り替え後の SSE 再接続は未検証（実機ゲート未実施、D-LC-01〜03）。

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

確認依頼を送ると、偽の Hermes は AI指摘を `annotations.json` に書きます。基本の2件に加えて、1行に複数の文がある行があれば、その2文目だけを直す指摘を2件（`edits` を使うものと、直した文だけを `suggestion` にしたもの）書きます。1件は行番号をわざとずらしてあり、`quote` による位置合わせを確かめられます。`-modify-annotation-target` を付けて起動すると対象ファイルに1行追記するので、確認中にファイルが変更されたときの警告を確かめられます。

## エージェントに確認を依頼する（AI指摘）

レビューする前に、エージェントに文書を読ませて、確認が必要な箇所に指摘を付けてもらえます。たとえば、技術的に誤っていそうな箇所を探させ、その指摘を手がかりに自分でレビューします。エージェントが付けた指摘を、reviewer では AI指摘と呼びます。使うには、前節の Hermes Agent との連携を設定しておく必要があります。

### 使い方

1. ヘッダーの「🔍 AIに確認を依頼」を押します。
2. プリセットを選ぶと確認内容が入るので、必要なら書き換えます。組み込みのプリセットは次の2つです。
   - 「技術的な誤りの検出」：事実や技術仕様の誤りを、根拠の URL 付きで指摘するよう求めます。
   - 「誤訳の修正」：開いているファイルを翻訳文として、原文と突き合わせて誤訳を指摘するよう求めます。原文は、同じリポジトリの翻訳元のファイル（`content/ja/...` に対する `content/en/...` など）か、文書の中の原文から探します。
3. 対象範囲（全体、現在のファイル、選択したファイル）と送信先を選んで送ります。送信先の既定は新しいセッションです。「セッション名（空欄なら自動）」に名前を入力できます。空欄なら `reviewer: docs Q-1 技術的な誤りの検出` のような名前を付け、同じ名前があれば末尾に ` (2)` などを付けます。入力した名前がすでに使われている場合は、エラーを確認して別の名前を入力してください。執筆に使ったセッションに自分の文章を点検させたいときは、バインド済みのセッションを選びます。
4. 概要ページの「AI確認依頼」に、作業ログがリアルタイムに流れます。承認カードと停止ボタンは、フィードバックを送ったときと同じように使えます。
5. 完了すると、ファイル表示の行番号の横にマークが付きます。色は重要度（重大、要修正、軽微、確認推奨）、濃さは確信度を表します。マークを押すと、指摘の本文、根拠のリンク、修正案が開きます。
6. 使える指摘は「採用」します。本文、修正案、根拠を含んだ自分の下書きコメントになるので、編集してから通常どおり提出できます。不要な指摘は「却下」します。却下した指摘は「却下済みも表示」から元に戻せます。

エージェントには、対象ファイルを変更しないよう指示しています。それでも確認中にファイルが変わった場合は、その依頼に警告と差分へのリンクが出ます。

確認依頼を何度か送ると、AI指摘は依頼ごとに蓄積されます。概要ページで依頼ごとに表示を切り替えたり、未採用の指摘をまとめて破棄したりできます。

### プリセット

確認依頼のダイアログで「プリセットとして保存」を押すと、そのディレクトリの `.reviewer/presets.toml` に保存されます。どのディレクトリでも使いたいプリセットは、グローバル設定に書きます。

```toml
# ~/.config/reviewer/config.toml
[[presets]]
name = "用語の揺れ"
prompt = "同じ概念を指す用語の表記が揺れている箇所を指摘してください。"
scope = "all"          # all | current | selected
```

同じ名前のプリセットは、組み込み、グローバル、プロジェクトの順に後のものが優先されます。UI から編集や削除ができるのはプロジェクトのプリセットだけです。

### エージェントが書く形式

エージェントは `.reviewer/requests/<Q-n>/instructions.md` を読み、`annotations.json` を同じディレクトリに書きます。形式は instructions.md に書かれているので、エージェントには reviewer が送る指示文だけで足ります。

```json
{
  "request": "Q-1",
  "annotations": [
    {
      "path": "docs/ch1.md", "startLine": 12, "endLine": 12,
      "quote": ["この API は HTTP GET でデータを作成します。"],
      "severity": "major", "confidence": "high", "label": "must",
      "body": "GET は安全なメソッドとして定義されており、作成には POST を使います。",
      "evidence": [{ "url": "https://www.rfc-editor.org/rfc/rfc9110#section-9.2.1", "quote": "", "note": "" }],
      "suggestion": "この API は HTTP POST でデータを作成します。"
    }
  ],
  "summary": "全体の所見"
}
```

行番号がずれていても、`quote` を手がかりに位置を合わせます。

`suggestion` は、採用すると `quote` の行全体を置き換えます。行の一部（1行の中の1文など）だけを直す場合、エージェントは `suggestion` の代わりに `edits` を書きます。reviewer はそれを `quote` に当てはめ、行全体の置換案を作ります。

```json
"edits": [{ "find": "3種類のシグナルの1つ", "replace": "3種類のシグナルのうちの1つ" }]
```

エージェントが `suggestion` に直した文だけを書いてきた場合も、そのまま採用すると同じ行のほかの文が消えてしまいます。そこで reviewer は取り込み時にこれを検出し、`quote` の中でいちばん近い文だけを差し替えて、行全体の置換案に補完します。補完できない場合は置換案にせず、本文に「修正案（該当部分のみ）」として残します。どちらの場合も、AI指摘のカードに注意書きが表示されます。

## 設定

`~/.config/reviewer/config.toml`（グローバル）と `DIR/.reviewer/config.toml`（プロジェクト）を読み込みます。port、bind、public_url、roots、agent はサーバー全体の設定なので、グローバル設定にだけ書きます。labels、exclude、prompt_template、anchor、data_dir は、プロジェクトの設定で上書きできます。

```toml
# ~/.config/reviewer/config.toml（サーバー全体の設定）
roots = ["~"]               # 開けるディレクトリ
port = 7777
bind = "all"                # 既定。"127.0.0.1" にするとこのマシンからのみ
public_url = ""             # 公開 URL。空なら従来の直接接続用 URL を使う
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
