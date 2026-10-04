# C11 完了報告

モバイル対応は未完了（実機ゲート未実施）。この報告の完了範囲は Phase 3 / C11 です。

## 対応範囲

対象は `feat/mobile-responsive`、開始時の HEAD は `f704d2c`、作業ツリーは空でした。指定された C1〜C10 の10コミットを確認しました。指示書を全体確認し、統合仕様の「compactレイアウト」「タッチ操作」と C11 の詳細に従いました。ゲート0節に残る C11/C12 の古い番号は、今回の明示指示とチェックポイント詳細に合わせて解釈しています。

- compact（360/412）だけでソース／プレビューをタブ表示します。medium（768）と expanded touch（1024）は左右分割です。タブの ARIA、選択状態、矢印／Home／End、スクロール比率復元を追加しました。非表示側のDOMを保持し、コメント下書きを保持します。
- narrow のファイルヘッダーではパスを1行目に置き、編集／ファイルコメント／プレビュー／補助操作を次の行に配置します。補助操作は重要度・却下済み・折り返しをまとめ、開いたときのフォーカス、Escape、外側タップ、ルート変更による閉じ方を扱います。expanded は `display: contents` と既存の表示順を維持します。
- Composer と ReplyForm に共通の `useKeyboardReveal` を追加しました。narrow または touch のときだけ、focus・入力・visualViewport の resize/scroll で操作欄を可視化します。保存・下書き処理には変更がありません。
- EditBar の操作を折り返し、破棄を主操作から離しました。EditReview は narrow で画面下のシートになり、safe-area、dvh、キーボード位置を考慮します。touch/narrow ではフォーカスと Escape を扱います。差分表示中とプレビュー表示中は SelectionBar の表示を抑えます。

## 画面ごとの幅の修正

| 対象 | 原因と対応 |
|---|---|
| FileView | nowrap の `min-width: max-content` がファイル全体を広げていました。narrow で最小幅を解除し、横スクロールは `.code` 内へ限定。スレッドは左端に固定し、compact の左余白を8pxへ変更しました。 |
| Preview | compact の分割列・固定高を解除。Markdownのpre・表・画像はペイン内に収めます。 |
| Composer / Thread | 操作列とラベル列を折り返し、compactで全幅にします。44pxターゲットと危険操作の8px以上の間隔を維持します。 |
| Settings | select の220px最小幅と52%最大幅をcompactで解除。設定行を縦積み、selectを全幅にします。既存クラスで対応できるためTSXの変更は不要でした。 |
| Overview | セクション見出し・コメント要約・長いパスを折り返します。フィードバックのパスとコピー操作、ラウンド表はカード内に収めます。 |
| RoundHistory | narrowでは固定320pxのサイド列を全幅の上段へ移し、差分本文を下段に置きます。パスのtitleも追加しました。 |
| Home | パス入力の最小幅を解除し、compactでプロジェクト行を縦積みにします。長いディレクトリ名を折り返し、一覧から外す操作を離します。 |
| CommentList | compactの余白を縮め、見出し・本文を折り返します。閉じるボタンとカードのタップ領域を確保します。 |
| AgentPanel | 実行ヘッダー・承認操作を折り返し、長い本文・コマンドの幅をパネル内に収めます。送信や承認は実行せず、表示用APIスタブで検証しました。 |

`style.css`、desktopの画像・スタイル基準、依存、API、ハッシュルート、localStorageキーは変更していません。Settings / Overview / Home / AgentPanel は既存クラスへの responsive.css の指定で対応しています。CommentList は、compact のプレビューから押したときにソースを表示してから移動する処理も追加しました。

## 実機判定と除外範囲

編集方式 `undecided`（既存インライン編集）、hidden時のSSE切断 `no`、interactive-widget `default`、タッチ行高 `adopt` を維持しました。実機レポートと性能判定は更新していません。C12以降のIME・性能最終化・受け入れ一式、PWA、Cookie変更は未着手です。

コミット・ステージ・push・stash・reset・checkout・restore・clean・rebase・merge・デプロイ・systemd操作は行っていません。E2EのXDG設定／状態／データは試験用ディレクトリに隔離しました。

## 検証結果

| 検証 | 結果 |
|---|---|
| `npm run typecheck` | ✓ |
| `npm test` | ✓ 9 files / 126 tests |
| `npm run typecheck:e2e` | ✓ |
| `go test ./...` | ✓ 全パッケージ |
| `npm run build` | ✓ dist再生成 |
| `npm run e2e` | 269 passed / 0 failed / 5 skipped |
| C11専用ケース（最終全回帰内） | ✓ 40 passed |
| desktop + boundary（最終全回帰内） | ✓ 35 passed |
| SSE（最終全回帰内） | ✓ 11 passed |
| distのspike検査 / `git diff --check` | ✓ 一致なし / エラーなし |

最終全回帰では C7/C8 下書き、C9/C10 行・文字範囲選択を含む全プロファイルを実行しました。tablet-landscape では narrow専用の既存ヘッダー・シート試験5件が従来の条件でskipされます。C11の試験にはskipはありません。

既知の tablet-landscape `viewport.e2e.ts` flaky は未変更です。今回の最終実行では両viewportケースとも成功し、flakyは再現しませんでした。実機Gboard・キーボード・選択ハンドルの合格を示す結果ではありません。

## 受け入れ条件とdesktop不変条件

| 条件 | 判定と証拠 |
|---|---|
| compactのみタブ、medium/expanded左右分割 | ✓ preview.e2e.ts、360/412/768/1024 |
| スクロール比率、active tab、下書き保持 | ✓ preview.e2e.ts |
| 補助メニュー、ARIA、フォーカス、Escape・外側タップ | ✓ preview.e2e.ts |
| Composer/返信/EditReviewのキーボード追従 | ✓ keyboard.e2e.ts（visualViewport高さ400pxを合成） |
| nowrapはコード内横スクロール、スレッドは画面内 | ✓ overflow.e2e.ts（narrow）。expandedの既存nowrap scrollerは維持 |
| Settings/Overview/RoundHistory/Home/CommentList/AgentPanel | ✓ overflow.e2e.ts、document幅・main幅も確認 |
| 44pxターゲット、危険操作の間隔、SelectionBarとの共存 | ✓ keyboard/overflowおよび既存touch-lines.e2e.ts |
| INV-1 外観 | ✓ 1440/1024/840、sidebar400を含む既存画像基準 |
| INV-2 計算済みスタイル | ✓ 既存JSON基準 |
| INV-3 マウスとキーボード | ✓ desktop interactions |
| INV-4 改行・BOM・末尾改行・409 | ✓ desktop edit |
| INV-5 1文字入力の行再描画数 | ✓ desktop edit |
| INV-6 API/ルート/localStorageキー | ✓ 対象差分のレビュー、Goテスト |
| INV-7 既存単体テスト | ✓ Go / Vitest |

意図的なdesktop挙動変更: なし。5.1の既存例外1〜6は維持しました。

## 検証過程で検出した問題と対応

- expandedの補助ボタンは `.btn` のdisplayでhiddenが上書きされたため、明示的な表示条件を追加しました。また、非表示ボタンに操作用 `.btn` クラスを付けないことで、既存の可視ターゲット検査を維持しました。既存C9テストは変更していません。
- コメント一覧の移動先がプレビュータブの裏に隠れる組み合わせを修正し、人間コメントとAI指摘の両方を検証しました。
- 最初の全回帰は261 passed / 8 failed / 5 skippedでした。1件は上記の非表示ターゲット、7件は並行試験サーバーとSSE専用サーバーの17779ポート競合でした。最終全回帰は並行E2Eを止めて実行しました。
- desktopを別のE2E_ROOTで試したときは、選択画面のパスマスクの幅が変わって差分になりました。既定の `/tmp/reviewer-e2e` で再実行し、基準を変更せず一致を確認しました。
- 新規試験の返信には提出済みコメントが必要なため、保存完了・提出後に返信を開くよう試験準備を修正しました。設定のGETスタブも実際の `/api/git` に合わせました。
- expanded touchで非常に長い送信先名を含む既存ヘッダーが幅1067pxになるケースを追加検証中に観測しました。C11のcompact化とパネル本文とは別の既存ヘッダー配置なので変更していません。パネル試験は通常長の送信先名と長い本文・承認説明・コマンドで検証しています。

## スクリーンショットとログ

最終の画面別画像は `/tmp/reviewer-c11-regression/` の各テストディレクトリに保存しています。360/412の `compact-preview-tab.png`、`composer-keyboard.png`、`settings.png`、`overview.png`、`round-review.png`、`round-agent.png`、`home.png`、コメント一覧、`agent-panel.png` を含みます。プレビュー、キーボード時Composer、設定、Homeの画像も目視で確認しました。

コマンドの完全なログは `/tmp/reviewer-c11-logs/` にあります。最終結果は `regression.log` を根拠にしています。`regression-first.log` と `c11-standalone-before-last-fixes.log` は修正途中の記録です。


## コマンド出力（各末尾20行）

### `cd web && npm run typecheck`

```text

> reviewer-web@0.0.0 typecheck
> tsc --noEmit

```

### `cd web && npm test`

```text

> reviewer-web@0.0.0 test
> vitest run


 RUN  v4.1.11 /home/ymotongpoo/repos/reviewer/web

(node:65) ExperimentalWarning: localStorage is not available because --localstorage-file was not provided.
(Use `node --trace-warnings ...` to show where the warning was created)

 Test Files  9 passed (9)
      Tests  126 passed (126)
   Start at  06:13:40
   Duration  1.14s (transform 515ms, setup 0ms, import 863ms, tests 326ms, environment 1ms)

```

### `cd web && npm run typecheck:e2e`

```text

> reviewer-web@0.0.0 typecheck:e2e
> tsc -p e2e/tsconfig.json --noEmit

```

### `GOCACHE=/tmp/reviewer-go-build go test ./...`

```text
?   	github.com/ymotongpoo/reviewer/cmd/fakehermes	[no test files]
ok  	github.com/ymotongpoo/reviewer/cmd/reviewer	(cached)
?   	github.com/ymotongpoo/reviewer/internal/agent	[no test files]
ok  	github.com/ymotongpoo/reviewer/internal/agent/hermes	(cached)
?   	github.com/ymotongpoo/reviewer/internal/agent/hermes/hermestest	[no test files]
ok  	github.com/ymotongpoo/reviewer/internal/anchor	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/annotate	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/app	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/config	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/feedback	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/gitops	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/project	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/server	0.634s
ok  	github.com/ymotongpoo/reviewer/internal/store	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/textutil	(cached)
```

### `cd web && npm run build`

```text
../internal/server/dist/assets/html-CtK9hRNn.js                           57.30 kB │ gzip:  11.78 kB
../internal/server/dist/assets/markdown-BYOwaDjH.js                       59.32 kB │ gzip:   5.66 kB
../internal/server/dist/assets/python-gzcpVVnB.js                         69.94 kB │ gzip:   9.09 kB
../internal/server/dist/assets/c-CnaQVCJy.js                              72.16 kB │ gzip:  10.55 kB
../internal/server/dist/assets/swift-CyEgAFGc.js                          87.22 kB │ gzip:  14.79 kB
../internal/server/dist/assets/latex-ZgXIZN55.js                          89.15 kB │ gzip:  10.96 kB
../internal/server/dist/assets/csharp-oqKa8noW.js                         90.18 kB │ gzip:  10.71 kB
../internal/server/dist/assets/php-BDVUjHbn.js                           113.08 kB │ gzip:  28.85 kB
../internal/server/dist/assets/asciidoc-SCjQUq34.js                      136.04 kB │ gzip:   9.45 kB
../internal/server/dist/assets/mdx-DQZ5AkYe.js                           136.10 kB │ gzip:  23.54 kB
../internal/server/dist/assets/javascript-BgS3c2Ky.js                    174.82 kB │ gzip:  16.62 kB
../internal/server/dist/assets/tsx-C-6sew-s.js                           175.59 kB │ gzip:  16.67 kB
../internal/server/dist/assets/jsx-D10EnvV5.js                           177.84 kB │ gzip:  16.76 kB
../internal/server/dist/assets/typescript-CfBQBPov.js                    181.13 kB │ gzip:  16.28 kB
../internal/server/dist/assets/katex-DnD4SCnU.js                         258.89 kB │ gzip:  77.66 kB
../internal/server/dist/assets/dist-D3N85Sbe.js                          297.38 kB │ gzip: 116.20 kB
../internal/server/dist/assets/index-Dm6yWwwN.js                         440.34 kB │ gzip: 145.27 kB
../internal/server/dist/assets/cpp-CN_Vgh-E.js                           796.96 kB │ gzip:  60.65 kB

✓ built in 742ms
```

### `cd web && E2E_SKIP_BUILD=1 npm run e2e:desktop -- --output=/tmp/reviewer-c11-desktop-final`

```text
  ✓  20 [desktop] › e2e/desktop/edit.e2e.ts:204:1 › a late response from a previous file cannot clear the current edit or its journal (2.0s)
  ✓  21 [desktop] › e2e/desktop/inputlog.e2e.ts:4:1 › input log is opt-in, persists the flag, exports metadata and can be disabled (1.2s)
  ✓  22 [desktop] › e2e/desktop/interactions.e2e.ts:7:1 › C9 leaves fine-pointer gutters and selection UI unchanged, including narrow windows (900ms)
  ✓  23 [desktop] › e2e/desktop/interactions.e2e.ts:20:1 › gutter click and Shift-click extend a line comment (2.6s)
  ✓  24 [desktop] › e2e/desktop/interactions.e2e.ts:38:1 › gutter drag selects L3 through L6 (1.9s)
  ✓  25 [desktop] › e2e/desktop/interactions.e2e.ts:56:3 › selected text becomes a character range: guide.md (1.7s)
  ✓  26 [desktop] › e2e/desktop/interactions.e2e.ts:56:3 › selected text becomes a character range: emoji.md (1.7s)
  ✓  27 [desktop] › e2e/desktop/interactions.e2e.ts:84:1 › hover controls, double click, Ctrl+S, Escape and i (971ms)
  ✓  28 [desktop] › e2e/desktop/interactions.e2e.ts:108:1 › autosave after debounce, Ctrl+Enter, submit counts and tree navigation (1.9s)
  ✓  29 [desktop] › e2e/desktop/interactions.e2e.ts:126:1 › external file write arrives over SSE (1.0s)
  ✓  30 [desktop] › e2e/desktop/styles.e2e.ts:6:1 › computed desktop styles including hovered controls (2.1s)
  ✓  31 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 280 (6.5s)
  ✓  32 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1024x768 sidebar 280 (5.2s)
  ✓  33 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 840x900 sidebar 280 (5.0s)
  ✓  34 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 400 (6.2s)
(node:542) Warning: The 'NO_COLOR' env is ignored due to the 'FORCE_COLOR' env being set.
(Use `node --trace-warnings ...` to show where the warning was created)
  ✓  35 [boundary] › e2e/boundary/layout.e2e.ts:3:1 › 839px drawer returns to the desktop sidebar at 840px with a fine pointer (1.2s)

  35 passed (1.3m)
```

### `cd web && E2E_SKIP_BUILD=1 npm run e2e -- --output=/tmp/reviewer-c11-regression`

```text
  ✓  260 [tablet-landscape] › e2e/mobile/touch-range.e2e.ts:137:1 › changed text cannot open a comment and retains native selection on rejection (1.1s)
  ✓  261 [tablet-landscape] › e2e/mobile/touch-range.e2e.ts:153:1 › navigation cancels a pending selection read and removes the bar (856ms)
  ✓  262 [tablet-landscape] › e2e/mobile/viewport.e2e.ts:22:1 › touch viewport tracks resize, scroll and rotation with safe-area foundation styles (756ms)
  ✓  263 [tablet-landscape] › e2e/mobile/viewport.e2e.ts:62:3 › fine pointer viewport › leaves expanded desktop unset and clears variables after returning from narrow (500ms)
(node:4627) Warning: The 'NO_COLOR' env is ignored due to the 'FORCE_COLOR' env being set.
(Use `node --trace-warnings ...` to show where the warning was created)
  ✓  264 [sse] › e2e/sse/resync.e2e.ts:59:1 › restart resyncs a file changed while the server was stopped (9.0s)
  ✓  265 [sse] › e2e/sse/resync.e2e.ts:69:1 › a delayed old snapshot cannot overwrite the newer sync or buffered comments (5.6s)
  ✓  266 [sse] › e2e/sse/resync.e2e.ts:97:1 › visible return refetches project state and retains the one live EventSource (4.0s)
  ✓  267 [sse] › e2e/sse/resync.e2e.ts:111:1 › resync preserves the edit base and draft, so saving against external changes returns 409 (13.4s)
  ✓  268 [sse] › e2e/sse/resync.e2e.ts:129:1 › SSE comments and a visibility sync preserve the unsaved Composer body (5.1s)
  ✓  269 [sse] › e2e/sse/resync.e2e.ts:150:1 › close preserves the existing API behavior: the next GET reopens the project (3.9s)
  ✓  270 [sse] › e2e/sse/resync.e2e.ts:160:1 › a forgotten project returns 404 on resync and shows unavailable with a picker link (974ms)
  ✓  271 [sse] › e2e/sse/resync.e2e.ts:170:1 › restart with a different token yields unauthorized without discarding the page (6.1s)
  ✓  272 [sse] › e2e/sse/resync.e2e.ts:177:1 › resync restores a missed response banner and dismissal persists across reload (9.4s)
  ✓  273 [sse] › e2e/sse/resync.e2e.ts:200:1 › malformed SSE warns without an uncaught exception and a subsequent stream syncs (7.7s)
  ✓  274 [sse] › e2e/sse/resync.e2e.ts:217:1 › terminal errors before the first sync display the same guidance (764ms)

  5 skipped
  269 passed (8.9m)
```

## 変更ファイル

`git status --short`（報告書自身を含みます）:

```text
 D internal/server/dist/assets/c-CvUzdXKz.js
 D internal/server/dist/assets/cpp-D45R2WOR.js
 D internal/server/dist/assets/css-QkjsZcin.js
 D internal/server/dist/assets/dist-jBDmkQBx.js
 D internal/server/dist/assets/graphql-BzU218hv.js
 D internal/server/dist/assets/html-BLWnERgK.js
 D internal/server/dist/assets/html-derivative-PintL_jq.js
 D internal/server/dist/assets/index-BGrwg4w5.css
 D internal/server/dist/assets/index-di12Y2zE.js
 D internal/server/dist/assets/java-CJpfaI7m.js
 D internal/server/dist/assets/jsx-DjNlLkP_.js
 D internal/server/dist/assets/lua-DKRTCEbl.js
 D internal/server/dist/assets/php-BbprC97a.js
 D internal/server/dist/assets/rst-CKKPE4A0.js
 D internal/server/dist/assets/ruby-BHbpSyM1.js
 D internal/server/dist/assets/sql-CI6JQhYe.js
 D internal/server/dist/assets/tsx-H1NTq4mH.js
 D internal/server/dist/assets/typescript-CF_H6J2l.js
 D internal/server/dist/assets/vue-B1i9bZP7.js
 D internal/server/dist/assets/xml-BxPEBvsP.js
 M internal/server/dist/index.html
 M web/src/components/CommentList.tsx
 M web/src/components/Composer.tsx
 M web/src/components/EditBar.tsx
 M web/src/components/FileView.tsx
 M web/src/components/Preview.tsx
 M web/src/components/RoundHistory.tsx
 M web/src/components/Thread.tsx
 M web/src/responsive.css
?? docs/mobile/c11-report.md
?? internal/server/dist/assets/c-CnaQVCJy.js
?? internal/server/dist/assets/cpp-CN_Vgh-E.js
?? internal/server/dist/assets/css-CWOklFHK.js
?? internal/server/dist/assets/dist-D3N85Sbe.js
?? internal/server/dist/assets/graphql-BYdzKRds.js
?? internal/server/dist/assets/html-CtK9hRNn.js
?? internal/server/dist/assets/html-derivative-CL8eyWzP.js
?? internal/server/dist/assets/index-BHLGoktc.css
?? internal/server/dist/assets/index-Dm6yWwwN.js
?? internal/server/dist/assets/java-nE46b797.js
?? internal/server/dist/assets/jsx-D10EnvV5.js
?? internal/server/dist/assets/lua-CeFzWeKa.js
?? internal/server/dist/assets/php-BDVUjHbn.js
?? internal/server/dist/assets/rst-mT5PCP1D.js
?? internal/server/dist/assets/ruby-CWxsEO8d.js
?? internal/server/dist/assets/sql-BdZGjeyY.js
?? internal/server/dist/assets/tsx-C-6sew-s.js
?? internal/server/dist/assets/typescript-CfBQBPov.js
?? internal/server/dist/assets/vue-DGyA6R7b.js
?? internal/server/dist/assets/xml-DoRGJBJO.js
?? web/e2e/mobile/keyboard.e2e.ts
?? web/e2e/mobile/overflow.e2e.ts
?? web/e2e/mobile/preview.e2e.ts
?? web/src/components/useKeyboardReveal.ts
```

`git diff --stat`（未追跡ファイルの行数は含みません）:

```text
 internal/server/dist/assets/c-CvUzdXKz.js          |   1 -
 internal/server/dist/assets/cpp-D45R2WOR.js        |   1 -
 internal/server/dist/assets/css-QkjsZcin.js        |   1 -
 internal/server/dist/assets/dist-jBDmkQBx.js       |  63 ------
 internal/server/dist/assets/graphql-BzU218hv.js    |   1 -
 internal/server/dist/assets/html-BLWnERgK.js       |   1 -
 .../server/dist/assets/html-derivative-PintL_jq.js |   1 -
 internal/server/dist/assets/index-BGrwg4w5.css     |   1 -
 internal/server/dist/assets/index-di12Y2zE.js      | 252 ---------------------
 internal/server/dist/assets/java-CJpfaI7m.js       |   1 -
 internal/server/dist/assets/jsx-DjNlLkP_.js        |   1 -
 internal/server/dist/assets/lua-DKRTCEbl.js        |   1 -
 internal/server/dist/assets/php-BbprC97a.js        |   1 -
 internal/server/dist/assets/rst-CKKPE4A0.js        |   1 -
 internal/server/dist/assets/ruby-BHbpSyM1.js       |   1 -
 internal/server/dist/assets/sql-CI6JQhYe.js        |   1 -
 internal/server/dist/assets/tsx-H1NTq4mH.js        |   1 -
 internal/server/dist/assets/typescript-CF_H6J2l.js |   1 -
 internal/server/dist/assets/vue-B1i9bZP7.js        |   1 -
 internal/server/dist/assets/xml-BxPEBvsP.js        |   1 -
 internal/server/dist/index.html                    |   4 +-
 web/src/components/CommentList.tsx                 |  21 +-
 web/src/components/Composer.tsx                    |   4 +-
 web/src/components/EditBar.tsx                     |  10 +-
 web/src/components/FileView.tsx                    | 110 ++++++---
 web/src/components/Preview.tsx                     |  15 +-
 web/src/components/RoundHistory.tsx                |   2 +-
 web/src/components/Thread.tsx                      |   4 +-
 web/src/responsive.css                             |  99 ++++++++
 29 files changed, 219 insertions(+), 383 deletions(-)
```

## 人間がステージする場合の対象パス

以下は一覧だけであり、ステージ操作は実施していません。再生成したdistの旧ファイル削除と新ファイル追加を含みます。

```text
internal/server/dist/assets/c-CvUzdXKz.js
internal/server/dist/assets/cpp-D45R2WOR.js
internal/server/dist/assets/css-QkjsZcin.js
internal/server/dist/assets/dist-jBDmkQBx.js
internal/server/dist/assets/graphql-BzU218hv.js
internal/server/dist/assets/html-BLWnERgK.js
internal/server/dist/assets/html-derivative-PintL_jq.js
internal/server/dist/assets/index-BGrwg4w5.css
internal/server/dist/assets/index-di12Y2zE.js
internal/server/dist/assets/java-CJpfaI7m.js
internal/server/dist/assets/jsx-DjNlLkP_.js
internal/server/dist/assets/lua-DKRTCEbl.js
internal/server/dist/assets/php-BbprC97a.js
internal/server/dist/assets/rst-CKKPE4A0.js
internal/server/dist/assets/ruby-BHbpSyM1.js
internal/server/dist/assets/sql-CI6JQhYe.js
internal/server/dist/assets/tsx-H1NTq4mH.js
internal/server/dist/assets/typescript-CF_H6J2l.js
internal/server/dist/assets/vue-B1i9bZP7.js
internal/server/dist/assets/xml-BxPEBvsP.js
internal/server/dist/index.html
web/src/components/CommentList.tsx
web/src/components/Composer.tsx
web/src/components/EditBar.tsx
web/src/components/FileView.tsx
web/src/components/Preview.tsx
web/src/components/RoundHistory.tsx
web/src/components/Thread.tsx
web/src/responsive.css
docs/mobile/c11-report.md
internal/server/dist/assets/c-CnaQVCJy.js
internal/server/dist/assets/cpp-CN_Vgh-E.js
internal/server/dist/assets/css-CWOklFHK.js
internal/server/dist/assets/dist-D3N85Sbe.js
internal/server/dist/assets/graphql-BYdzKRds.js
internal/server/dist/assets/html-CtK9hRNn.js
internal/server/dist/assets/html-derivative-CL8eyWzP.js
internal/server/dist/assets/index-BHLGoktc.css
internal/server/dist/assets/index-Dm6yWwwN.js
internal/server/dist/assets/java-nE46b797.js
internal/server/dist/assets/jsx-D10EnvV5.js
internal/server/dist/assets/lua-CeFzWeKa.js
internal/server/dist/assets/php-BDVUjHbn.js
internal/server/dist/assets/rst-mT5PCP1D.js
internal/server/dist/assets/ruby-CWxsEO8d.js
internal/server/dist/assets/sql-BdZGjeyY.js
internal/server/dist/assets/tsx-C-6sew-s.js
internal/server/dist/assets/typescript-CfBQBPov.js
internal/server/dist/assets/vue-DGyA6R7b.js
internal/server/dist/assets/xml-DoRGJBJO.js
web/e2e/mobile/keyboard.e2e.ts
web/e2e/mobile/overflow.e2e.ts
web/e2e/mobile/preview.e2e.ts
web/src/components/useKeyboardReveal.ts
```

提案するコミットメッセージ: `feat: adapt file view and pages to compact layout`

判断を仰ぐ事項: C11の実装に関する追加判断はありません。実機ゲートは未実施のままです。

次のチェックポイントはC12のIME対策ですが、今回の作業はC11で停止します。
