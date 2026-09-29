# reviewer: エージェントに確認を依頼して指摘を付けてもらう機能（AI指摘）

## Context

reviewer は現在、人間 → エージェントの一方向（人間がコメントを書き、提出し、エージェントが直して `response.json` で返す）しか持たない。
人間が他人の文書をレビューする場面では、先にエージェントに「技術的に誤っていそうな箇所」などを探させ、怪しい箇所に行コメントを付けてもらい、それを見ながら人間がレビューする流れが欲しい。

そのために次を追加する。

- reviewer から Hermes に任意のプロンプト（プリセット＋自由記述）で「確認依頼」を送る UI
- エージェントが書いた指摘（AI指摘 = annotation）を取り込み、ファイル上にマークだけ表示し、クリックで中身を読める表示
- 気に入った指摘を「採用」すると自分の下書きコメントになり、通常のラウンドで提出できる

## 合意した仕様

| 項目 | 決定 |
|---|---|
| 指摘の扱い | 未採用の指摘は普段は本文を出さず、行のマークだけ表示。マークをクリックで展開。「採用」で人間の下書きコメントに変換（編集可）。「却下」で非表示 |
| 指示 UI | 確認依頼専用ダイアログ。プリセット＋自由記述。対象範囲（全体／選択ファイル／現在のファイル）を指定 |
| プリセット | UI から保存・編集し、プロジェクトごとに `.reviewer/presets.toml` に保存。グローバル設定の `[[presets]]` と組み込みの既定プリセットも読み取り専用で並べる |
| 送信先 | 毎回選べる。既定は新規セッション。バインド済みセッション、または別の既存セッションも選べる |
| ファイル編集 | プロンプトで編集禁止を指示し、依頼時と完了時のスナップショットを比較。変更があれば警告と差分リンクを出す |
| 指摘の項目 | 本文、重要度（severity）、確信度（high/medium/low）、根拠（URL・引用・メモ）、修正案（suggestion）、提案ラベル（既存の must/suggestion/question/nit） |
| 重要度 | 4段階: `critical`（重大：事実誤り・動かない）、`major`（要修正）、`minor`（軽微）、`info`（確認推奨）。マークの色に使う |
| 再実行 | 指摘は依頼ごとに蓄積。依頼単位で表示オンオフ、未採用分の一括破棄ができる |

ラウンドとの関係：確認依頼と AI指摘はラウンドから独立して存在する。採用したときだけ、現在のラウンドの下書きコメントになる（`ensureOpen` を通る）。

## データ

```
.reviewer/
  presets.toml                 # プロジェクトのプリセット（UI が書く）
  annotations.jsonl            # AI指摘とその状態のイベントログ（comments.jsonl と同じ追記方式）
  requests/<Q-n>/
    request.json               # プロンプト、プリセット名、対象ファイル→blob、送信先、作成時刻、完了時の変更検出結果
    instructions.md            # エージェントが読む指示本体
    annotations.json           # エージェントが書く
    agent-runs.jsonl           # 既存と同じ形式
    agent-events.jsonl
```

`state.json` に `NextRequest` / `NextAnnotation` の採番を追加する。

`annotations.json`（エージェントが書く）:

```json
{
  "request": "Q-3",
  "annotations": [
    {
      "path": "docs/ch1.md", "startLine": 12, "endLine": 14,
      "quote": ["対象行の原文（そのまま）"],
      "severity": "major", "confidence": "high", "label": "must",
      "body": "指摘の本文（Markdown）",
      "evidence": [{ "url": "https://...", "quote": "出典の該当箇所", "note": "" }],
      "suggestion": "置換案（任意）"
    }
  ],
  "summary": "全体の所見（任意）"
}
```

取り込み時の位置決め：行番号は依頼時スナップショット基準。`startLine..endLine` の内容が `quote` と一致しなければ、スナップショット内で `quote` を探して移す。見つからなければ「位置不明」として依頼パネルに一覧表示する。その後は既存の `anchor.New` / `anchor.Resolve` で現在のファイルへ再アンカリングする（コメントと同じ `reanchor` の対象に加える）。

## 実装

### Go

- `internal/config/config.go`: `Preset{Name, Prompt, Scope}` と `Config.Presets`（グローバル）を追加。組み込みの既定プリセット「技術的な誤りの検出」を `Default()` に置く。
- `internal/store/annotation.go`（新規）: `Annotation`（ID, Request, Path, Orig*/Anchor/Loc は `Comment` と同じ型、Severity, Confidence, Label, Body, Evidence, Suggestion, State=`pending|adopted|dismissed`, AdoptedAs）、`AnnotationRequest`、jsonl の読み書き（`appendJSONL` / `readJSONL` を再利用）、`presets.toml` の読み書き。
- `internal/store/agent.go`: `AgentRun` に `Purpose`（`feedback` | `annotate`）と `Request` を追加。`PutAgentRun` / `AgentRuns` / `AppendAgentEvent` / `AgentEvents` の保存先を `Purpose` で `rounds/<N>/` か `requests/<Q-n>/` に切り替える。
- `internal/annotate/`（新規、`internal/feedback` と同じ役割）: `instructions.md` の生成（ユーザーのプロンプト、対象ファイルの絶対パス一覧、「ファイルを変更しない」規則、severity/confidence の定義、事実に関する指摘は根拠 URL 必須、`quote` 必須、`annotations.json` のスキーマと例）と、`annotations.json` の解析・検証（`feedback.ParseResponse` と同じく、不正エントリは落として警告にする。コードフェンスの許容も同じ）。
- `internal/app/annotate.go`（新規）:
  - `Annotate(ctx, req)`: 対象ファイルをスナップショット → `request.json` と `instructions.md` を書く → `agent.Start`（新規セッションなら `SessionID` 空）→ 既存の `follow` に乗せる。
  - `finish` を `Purpose` で分岐させ、annotate のときは `importAnnotations(reqID)` と編集検出（依頼時の blob と現在の比較）を行う。通知は既存セッションに送ったときだけ既存の notifier で出す（新規セッションには Discord スレッドがないため）。
  - `AdoptAnnotation(id, patch)`: 本文＋修正案（```` ```suggestion ````）＋根拠（リンクと引用）を組み立て、既存の `CreateComment` の内部処理で行コメントの下書きにする（ラベルは提案ラベル、無ければ severity から既定の対応表）。`AdoptedAs` を記録。
  - `SetAnnotationState`（dismissed / pending に戻す）、`DiscardRequest`（未採用分を dismissed に）、`SetRequestHidden`。
- `internal/app/watch.go`: `requests/*` を監視対象に加え、`annotations.json` の書き込みで取り込む。
- `internal/app/rounds.go` `reanchor`: pending の AI指摘も再アンカリングする。
- `internal/server/server.go`: 追加 API
  - `GET|PUT /api/presets`
  - `POST /api/annotate`、`GET /api/annotate/requests`、`GET /api/annotate/requests/{id}`（run とイベント込み）、`PATCH /api/annotate/requests/{id}`（hidden）、`POST /api/annotate/requests/{id}/discard`
  - `GET /api/annotations`、`POST /api/annotations/{id}/adopt`、`PATCH /api/annotations/{id}`
  - 停止・承認は既存の `/api/agent/runs/{id}/stop|approval` をそのまま使う（`active` は run ID で引くため変更不要）
  - SSE に `annotations` / `annotate` イベントを追加
- `cmd/fakehermes` / `internal/agent/hermes/hermestest/fake.go`: プロンプトが確認依頼なら `instructions.md` を読み、指摘2件（1件は quote をずらして再配置を確かめる）の `annotations.json` を書く。オプションで対象ファイルを1行書き換え、編集検出の警告を確かめられるようにする。

Hermes の新規セッション（`~/hermes/hermes-agent/gateway/platforms/api_server_runs.py` と `api_server.py` で確認済み）：

- `/v1/runs` に `session_id` を付けないと、`session_id = run_id` の新しいセッションになる（`api_server_runs.py` の `session_id = selected_session_id or run_id`）。
- タイトルを付けるには、先に `POST /api/sessions` に `{"title": "...", "source": "api_server"}` を送って空のセッションを作り、返った ID を `session_id` にして `/v1/runs` を呼ぶ（`_handle_create_session`）。reviewer はこちらを使い、タイトルを `reviewer: Q-3 <プリセット名>` にする。
- そのため `agent.Agent` に任意インターフェース `SessionCreator{ CreateSession(ctx, title) (Session, error) }` を追加し、Hermes クライアントで実装する。偽 Hermes にも `POST /api/sessions` を追加する。

### Web（Preact）

- `components/Annotate.tsx`（新規）: ヘッダーの「🔍 AIに確認を依頼」ボタンとダイアログ。プリセット選択（既定・グローバル・プロジェクトを区別して表示）、プロンプト編集、「プリセットとして保存／上書き／削除」、対象範囲、送信先（新規／バインド済み／既存を選ぶ。選択は既存の `SessionPicker` を再利用）。
- `components/FileView.tsx`: 行番号の横に AI指摘のマーク（色 = severity、濃さ = confidence）。クリックでポップオーバーを開き、本文、根拠リンク、修正案の diff プレビュー（既存の suggestion 表示を再利用）、「採用」「却下」を出す。採用済みの指摘は通常コメントに置き換わるのでマークを消す。
- `components/Tree.tsx`: ファイルごとに未採用の AI指摘の件数を出す。
- `components/Overview.tsx`: 「AI確認依頼」パネル。依頼ごとにプロンプト、状態、件数、警告（ファイル変更・位置不明・スキーマ違反）、作業ログ（`AgentPanel` の run 表示を流用）、表示オンオフ、未採用の一括破棄。
- `api.ts` / `state.ts` / `types.ts`: 対応する型、API、SSE ハンドリング。表示中の依頼や severity の絞り込みは localStorage に保存。

### ドキュメント

- `SPEC.md` に「AI確認依頼とAI指摘」の節を追加し、§12 の対象外リストを見直す。
- `README.md` に使い方とプリセット設定例を追加。
- 承認後、この計画を `reviewer/docs/plans/` に保存する。

## 進め方

Claude が計画と指示を作り、具体的なコードは Codex（codex:codex-rescue）に書かせる。コミットの区切り：

1. C1: config（Preset）と store（Annotation、Request、AgentRun の Purpose、presets.toml）
2. C2: `internal/annotate`（instructions.md 生成と annotations.json 解析）
3. C3: app（確認依頼の送信、finish の分岐、取り込み、再アンカリング、採用・却下、編集検出）
4. C4: server API と SSE
5. C5: fakehermes の確認依頼対応
6. C6: Web UI
7. C7: SPEC.md / README.md

## 検証

- 単体テスト（`make test`）
  - `annotations.json` の解析：正常、不明な severity/confidence、quote 不一致の再配置、位置不明、重複、コードフェンス付き
  - `instructions.md` のゴールデンファイル比較
  - app：確認依頼 → 偽 Hermes が `annotations.json` を書く → 取り込み → 採用で下書きコメントができ、次の提出の `feedback.md` に含まれる → 却下で一覧から消える
  - 実行中に対象ファイルが変わったときの警告
  - ファイル変更後の AI指摘の再アンカリング
  - `presets.toml` の保存と、グローバル／既定プリセットとの合成
- `cd web && npm run build`（型チェック込み）
- 手動 E2E：`fakehermes` と `reviewer serve ./sample` を起動し、ブラウザで依頼 → マーク表示 → 展開 → 採用 → 提出までを確認（webapp-testing の Playwright でスクリーンショット）
- 実機：本物の Hermes に、技術的な誤りを仕込んだサンプル Markdown で「技術的な誤りの検出」プリセットを新規セッションに送り、指摘が根拠 URL 付きで取り込まれ、ファイルが変更されないことを確認する
