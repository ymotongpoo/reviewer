# Phase 0 スパイク記録

このレポートは実機ゲートの代わりにならない。

2026-10-04に `feat/mobile-responsive` のC1コミット `e68bb21` を基準として、C2の計測コードを加えた作業ツリーで測定しました。Android実機とadbは未使用です。実行環境はLinux、Node.js v26.8.1、Go go1.26.0、Playwright 1.63.0、Chromium 153.0.8010.12です。Node.jsは指示書の環境記録v24.13.1と異なり、今回のコマンド実行環境でv26.8.1を確認しています。ランタイム依存の追加はありません。

`E2E_SKIP_BUILD=1 npm run e2e:spike` は12件すべて成功しました。事前に `npm run build` で生成したGo埋め込み用distを使っています。12件はS-01〜S-11の11件と計測ページの動作確認1件です。各測定は `test.info().attach` のJSONと標準出力に記録し、この文書の末尾にも生データを保存しました。

S-01、S-05、S-06、S-09はモバイル設定のChromiumです。S-02は3条件を比較し、S-03、S-04、S-07、S-08、S-10、S-11はデスクトップChromiumです。UAの変更、タッチ入力、CDPの合成はGboardの実装やAndroidのプロセス管理を再現しません。XDGの設定、状態、データ、キャッシュと永続ブラウザープロファイルは `/tmp/reviewer-e2e` 配下に分離しました。

## S-01 ルート別の幅

360×800と412×915の `isMobile + hasTouch` コンテキストで、ルート表示後のdocument scrollWidthとinnerWidthを取得しました。誤判定を避けるためclientWidthとvisualViewportの幅も記録しています。

| 設定幅 | ルート | 状態 | scrollWidth | innerWidth | clientWidth |
|---|---|---|---:|---:|---:|
| 360 | `/` | default | 360 | 360 | 360 |
| 360 | `#/` | default | 637 | 637 | 360 |
| 360 | `#/file/guide.md` | default | 637 | 637 | 360 |
| 360 | `#/file/guide.md` | preview-split (tabs do not exist in C2) | 637 | 637 | 360 |
| 360 | `#/file/long.txt` | default | 637 | 637 | 360 |
| 360 | `#/file/long.txt` | nowrap | 637 | 637 | 360 |
| 360 | `#/file/long.txt` | wrap | 637 | 637 | 360 |
| 360 | `#/round/1?view=review` | default | 637 | 637 | 360 |
| 360 | `#/round/1?view=agent` | default | 637 | 637 | 360 |
| 360 | `#/round/1/guide.md?view=review` | default | 637 | 637 | 360 |
| 360 | `#/settings` | default | 637 | 637 | 360 |
| 412 | `/` | default | 412 | 412 | 412 |
| 412 | `#/` | default | 651 | 651 | 412 |
| 412 | `#/file/guide.md` | default | 651 | 651 | 412 |
| 412 | `#/file/guide.md` | preview-split (tabs do not exist in C2) | 651 | 651 | 412 |
| 412 | `#/file/long.txt` | default | 651 | 651 | 412 |
| 412 | `#/file/long.txt` | nowrap | 651 | 651 | 412 |
| 412 | `#/file/long.txt` | wrap | 651 | 651 | 412 |
| 412 | `#/round/1?view=review` | default | 651 | 651 | 412 |
| 412 | `#/round/1?view=agent` | default | 651 | 651 | 412 |
| 412 | `#/round/1/guide.md?view=review` | default | 651 | 651 | 412 |
| 412 | `#/settings` | default | 651 | 651 | 412 |

選択画面以外では設定幅を277pxまたは239px超えています。innerWidthも広がるため、scrollWidthとinnerWidthの比較だけでは全項目が通ってしまいます。visualViewportの幅は360pxまたは412px、scaleは1でした。仮判定はレイアウト修正が必要です。後続の受け入れ検証ではclientWidthや設定幅も確認する必要があります。

C2にはプレビューのタブがないため、現行の左右分割プレビューを測定しました。実機のアドレスバー、分割画面、拡大表示は未検証です。

## S-02 メディア特性

`isMobile + hasTouch`、`hasTouch`のみ、デスクトップの3条件でmatchMediaを取得しました。その後CDPでpointer=fine、hover=hover、any-pointer=coarseを要求しました。

| 条件 | pointer | hover | any-pointer:coarse | any-hover:hover | CDP要求後の変化 |
|---|---|---|---|---|---|
| isMobile + hasTouch | coarse | none | true | false | なし |
| hasTouchのみ | coarse | none | true | false | なし |
| desktop | fine | hover | false | true | なし |

CDPは例外を返しませんが、このブラウザーでは指定した組み合わせになりませんでした。仮判定として、タッチと通常マウスの比較にはコンテキスト設定を使えます。タッチパネル付きノートPCのfineとany-pointer:coarseの同居は、このCDP指定で再現できたとは扱いません。

## S-03 CDPによるIME

行末で `Input.imeSetComposition` を開始し、確定、変換中Enter、変換中Ctrl+Zを別々に実行しました。各ケースの終了後にCDPの合成状態を解除して、次のケースへの持ち越しを防いでいます。

| 操作 | 入力欄の結果 | 行数 | 観測 |
|---|---|---:|---|
| にほんご→日本語の確定 | 末尾改行なし日本語 | 1 | 日本語が1回入り、Undo1回で元の6文字に復帰 |
| 変換中Enter | 末尾改行なしにほんご | 1 | keydownとkeyupはisComposing=true。改行もcompositionendも発生せず |
| 変換中Ctrl+Z | 末尾改行なしにほんご | 1 | historyUndoのbeforeinputをdefaultPrevented=trueで観測 |

確定時の順序はcompositionstart、compositionupdate、beforeinput、input、compositionupdate、beforeinput、input、compositionendでした。この間にselectionchangeが入ります。insertCompositionTextのbeforeinputはcancelable=false、isComposing=trueでした。完全なイベント列は末尾のS-03にあります。

仮判定は、CDPの確定とUndoの検証は利用可能です。変換中Enterは候補確定まで再現していないため、Gboardの合格判定には使えません。変換中UndoもネイティブIMEとの比較が必要です。

## S-04 Android風の合成列

key=Unidentified、keyCode=229のkeydownに続けてbeforeinputを送りました。改行は行末、後方削除は行頭で実行しています。

| inputType | cancelable | keydownの抑止 | beforeinputの抑止 | 行数の前→後 |
|---|---|---|---|---|
| insertLineBreak | true | false | false | 25→25 |
| insertLineBreak | false | false | false | 25→25 |
| deleteContentBackward | true | false | false | 25→25 |
| deleteContentBackward | false | false | false | 25→25 |

入力欄の16文字にも変化はありませんでした。合成イベントには既定動作がないので、ハンドラーの論理を確かめているだけです。仮判定として、現行ハンドラーはこの列で改行や行結合を実行していません。cancelable=falseの場合に実ブラウザーが生成する後続inputは、この検証では発生させていません。

## S-05 ガターと文字選択

モバイル設定でL3のガターをタップするとComposerがすぐ開きました。次に `setBaseAndExtent` でL3本文16文字を選択してからタップすると、保存されたrangeはL3の列0〜16で、本文と一致しました。タップ後のDOM選択は空になっています。

仮判定は、互換マウスイベント経由のガター操作と、プログラムで選択した範囲の引き継ぎは再現できました。Androidの長押し、選択ハンドル、標準メニューとタップの競合は再現していません。

## S-06 行移動時のフォーカス

現行ヘッダーの「編集」から入力を開始し、L4本文をタップしました。activeElementはL4の `.inline-input` でした。

仮判定はDOMのフォーカス移動を検証可能です。ソフトウェアキーボードの表示維持、かなと英数のモード維持、ちらつきは実機で確認します。

## S-07 SSEの復帰と異常入力

ハーネスとは別の一時サーバーを17779番で起動し、停止中にファイルを書き換えてから再起動しました。ストリームの200応答は2回、ファイル取得は2回で、再接続後の画面はサーバー上の変更済み本文と一致しました。

次にclose APIを呼び、3.5秒後も画面にファイル本文が残ることを観測しました。確認用のproject APIは200でした。現行のRegistry.Getはrecentに残るプロジェクトを再オープンするため、closeだけで恒久的な404になるとは限りません。404を前提にする後続検証ではこのサーバー動作を考慮する必要があります。C2では製品コードを変更していません。

別ページでeventsを `data: {broken` に差し替えると、JSON.parse由来のpageerrorが1件発生しました。仮判定は、通常再起動時の復帰を確認できた一方、異常JSONの保護が必要です。OSによるバックグラウンド停止やネットワーク切替は未検証です。

## S-08 別サイトからのCookie

127.0.0.1のトークン付きURLでCookieを作り、別ポートのlocalhostページにあるリンクから127.0.0.1へ遷移しました。

| 条件 | HTTP状態 | Cookie |
|---|---:|---|
| localhostからのリンク | 401 | 送信なし |
| 続いて直接URLを開く | 200 | 認証成功 |

Cookieの属性はSameSite=Strict、HttpOnly=true、Secure=falseでした。仮判定は別サイトからの初回アクセス失敗を再現できました。Discord内ブラウザー、Tailscale HTTPS、実機のCookie保持は未検証です。Cookie属性は変更していません。

## S-09 3000行の性能

412×915のモバイル設定でCPUを4倍に制限し、A（現行CSS）とB（行のmin-height:44px、✎と+を常時表示）を交互に各5回測定しました。各回は新しいブラウザーコンテキストを使っています。表示時間はハッシュ遷移開始からMutationObserverがL3000を検出するまでです。描画完了や全ハイライト完了までの時間ではありません。

スクロール測定はハイライトを待ってから開始し、`.main.scrollTop` を10段階で末尾まで動かし、各段階100ms待ってlongtaskを観測しました。慣性付きの指スワイプやGPUのフレーム時間は測っていません。

| 回 | CSS | 表示時間 ms | スクロール中の最大longtask ms | L3の高さ px |
|---|---|---:|---:|---:|
| 1 | A | 657.9 | 0.0 | 20 |
| 1 | B | 642.6 | 0.0 | 44 |
| 2 | A | 630.7 | 0.0 | 20 |
| 2 | B | 669.7 | 0.0 | 44 |
| 3 | A | 695.1 | 0.0 | 20 |
| 3 | B | 656.7 | 0.0 | 44 |
| 4 | A | 628.7 | 0.0 | 20 |
| 4 | B | 625.0 | 0.0 | 44 |
| 5 | A | 635.9 | 0.0 | 20 |
| 5 | B | 700.1 | 0.0 | 44 |

表示時間の中央値はAが635.9ms、Bが656.7msで、比率は1.033です。各回の最大longtaskの中央値は両方0msでした。0は50ms以上のlongtaskが観測されなかったことを表します。scrollHeightはAが60532px、Bが132532pxで、10段階のscrollTopが進んだことも記録しています。

Bの表示時間がAの1.5倍以下、スクロール中の最大longtaskが200ms以下という指示書の基準では、仮判定は `adopt` です。Android端末のCPU、GPU、発熱、実際の指スクロールに対する保証ではありません。ゲート0の選択前なので、製品の行高は変更していません。

## S-10 偽visualViewport

addInitScriptでEventTargetを持つvisualViewportに差し替え、高さ200px、offsetTop=40pxに変更してresizeを送信しました。resizeを1回受信し、可視下端は240pxでした。現行Composerの下部操作の下端は395.5pxで、155.5px隠れる位置に残りました。

仮判定は、偽visualViewportを後続の追従処理のテストに利用可能です。addInitScriptは次の文書読み込みから有効になるため、ハッシュ遷移だけの後には明示的な再読み込みが必要です。Androidのキーボードを開いた実測ではありません。

## S-11 IndexedDBの保持

一時プロファイルのlaunchPersistentContextで試験用本文とrevision=7をIndexedDBへ書き、トランザクション完了後にcontext.closeを行いました。同じプロファイルで別のブラウザープロセスを起動し、本文とrevisionが一致することを確認しました。

仮判定は永続プロファイルを使った再起動検証が可能です。今回の終了は正常終了で、Androidの強制終了、書き込み途中の停止、容量不足、ストレージ削除は未検証です。製品の下書き永続化は実装していません。

## 計測ページの確認

Viteの開発サーバーを127.0.0.1:5173に限定してテスト内で起動し、終了時に閉じました。2ページともコンソールエラーとpageerrorは0件でした。

viewportページでは初期1280×720でvh、svh、lvh、dvhがすべて720px、safe-areaは四辺0px、1remは16pxでした。360×800への変更で履歴追記を確認し、JSON保存とinteractive-widgetの両方向の切替も確認しました。Androidでのキーボード表示による差は未測定です。

ブロック編集では初期60行、2行の段落を3行に書き換えた後61行、変更3行という結果でした。「元に戻す」で元の60行との一致を確認しました。入力イベントにはtarget=blockが記録されました。C2のページは比較用で、製品の差分確認やファイル保存は含みません。D-BLK-14など、対応する操作がない項目は実機記録で該当なしと理由を記入します。

media.tsはC3で追加する予定のため、viewportページには指示書4.1の4定数と同じクエリを計測専用に定義しています。C3の製品用モジュールは先行作成していません。any-pointerとany-hoverはこの計測ページでの観測にだけ使っています。

## 実装中の検証修正

最初のスパイク実行は8件成功、4件失敗でした。新規テスト内のoverviewセレクターの重複、addInitScript後に文書読み込みを起こしていなかった箇所、試験用リンクページのUTF-8指定漏れを修正しました。S-03ではケース間のCDP合成状態の持ち越しも解消しました。型検査で検出したイベント登録と型指定も修正しています。既存C1テストと基準画像は変更していません。

## 次のチェックポイント

C2で停止します。次はゲート0で、A（実機ゲートを実施）かB（実機なしの仮判定で進行）を人間が選びます。[実機レポート](phase0-device-report.md)に編集方式、hidden時のSSE、interactive-widget、行高、通知URLを記入した後、C3のviewportとメディア判定へ進みます。Bを選ぶ場合も編集方式はundecidedで、限定ブロック編集の製品採用は行いません。

指示書のゲート0にはIMEの対象をC11とする箇所がありますが、チェックポイント本体ではC11が画面のcompact化、C12がIMEです。またS-01の参照先はC13と書かれていますが、完全なルート一覧はC14にあります。今回はC2の範囲内でC14の現存ルートを測定しました。これらの番号の食い違いはC2の実装内容を変えません。

## 測定の生データ

以下は最終スパイク実行の標準出力に出したJSONです。Playwrightの添付と同じ内容です。試験用データだけを含みます。

<details>
<summary>S-01</summary>

```json
[{"viewport":{"width":360,"height":800},"route":"/","state":"default","scrollWidth":360,"innerWidth":360,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/","state":"default","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/file/guide.md","state":"default","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/file/guide.md","state":"preview-split (tabs do not exist in C2)","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/file/long.txt","state":"default","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/file/long.txt","state":"nowrap","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/file/long.txt","state":"wrap","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/round/1?view=review","state":"default","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/round/1?view=agent","state":"default","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/round/1/guide.md?view=review","state":"default","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":360,"height":800},"route":"#/settings","state":"default","scrollWidth":637,"innerWidth":637,"clientWidth":360,"visualWidth":360,"scale":1},{"viewport":{"width":412,"height":915},"route":"/","state":"default","scrollWidth":412,"innerWidth":412,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/","state":"default","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/file/guide.md","state":"default","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/file/guide.md","state":"preview-split (tabs do not exist in C2)","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/file/long.txt","state":"default","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/file/long.txt","state":"nowrap","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/file/long.txt","state":"wrap","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/round/1?view=review","state":"default","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/round/1?view=agent","state":"default","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/round/1/guide.md?view=review","state":"default","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1},{"viewport":{"width":412,"height":915},"route":"#/settings","state":"default","scrollWidth":651,"innerWidth":651,"clientWidth":412,"visualWidth":412,"scale":1}]
```

</details>

<details>
<summary>S-02</summary>

```json
[{"name":"mobile-touch","before":{"(hover: none)":true,"(hover: hover)":false,"(pointer: coarse)":true,"(pointer: fine)":false,"(any-pointer: coarse)":true,"(any-hover: hover)":false},"afterCDP":{"(hover: none)":true,"(hover: hover)":false,"(pointer: coarse)":true,"(pointer: fine)":false,"(any-pointer: coarse)":true,"(any-hover: hover)":false}},{"name":"touch-only","before":{"(hover: none)":true,"(hover: hover)":false,"(pointer: coarse)":true,"(pointer: fine)":false,"(any-pointer: coarse)":true,"(any-hover: hover)":false},"afterCDP":{"(hover: none)":true,"(hover: hover)":false,"(pointer: coarse)":true,"(pointer: fine)":false,"(any-pointer: coarse)":true,"(any-hover: hover)":false}},{"name":"desktop","before":{"(hover: none)":false,"(hover: hover)":true,"(pointer: coarse)":false,"(pointer: fine)":true,"(any-pointer: coarse)":false,"(any-hover: hover)":true},"afterCDP":{"(hover: none)":false,"(hover: hover)":true,"(pointer: coarse)":false,"(pointer: fine)":true,"(any-pointer: coarse)":false,"(any-hover: hover)":true}}]
```

</details>

<details>
<summary>S-03</summary>

```json
[{"action":"commit","value":"末尾改行なし日本語","rows":1,"events":[{"t":289.5,"type":"compositionstart","data":"","cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":289.89999997615814,"type":"compositionupdate","data":"にほんご","cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":290.39999997615814,"type":"beforeinput","isComposing":true,"inputType":"insertCompositionText","data":"にほんご","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":291.09999999403954,"type":"input","isComposing":true,"inputType":"insertCompositionText","data":"にほんご","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":291.69999998807907,"type":"selectionchange","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":293.09999999403954,"type":"compositionupdate","data":"日本語","cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":293.19999998807907,"type":"beforeinput","isComposing":true,"inputType":"insertCompositionText","data":"日本語","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":293.69999998807907,"type":"input","isComposing":true,"inputType":"insertCompositionText","data":"日本語","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":9,"selEnd":9,"valueLen":9,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":296.5,"type":"compositionend","data":"日本語","cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":9,"selEnd":9,"valueLen":9,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":296.69999998807907,"type":"selectionchange","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":9,"selEnd":9,"valueLen":9,"vv":{"h":720,"top":0},"inner":{"h":720}}],"afterUndo":"末尾改行なし"},{"action":"Enter","value":"末尾改行なしにほんご","rows":1,"events":[{"t":452,"type":"compositionstart","data":"","cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":452.09999999403954,"type":"compositionupdate","data":"にほんご","cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":452.5,"type":"beforeinput","isComposing":true,"inputType":"insertCompositionText","data":"にほんご","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":452.7999999821186,"type":"input","isComposing":true,"inputType":"insertCompositionText","data":"にほんご","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":454.59999999403954,"type":"selectionchange","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":456.2999999821186,"type":"keydown","key":"Enter","code":"Enter","keyCode":13,"isComposing":true,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":459,"type":"keyup","key":"Enter","code":"Enter","keyCode":13,"isComposing":true,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}}],"afterUndo":null},{"action":"Control+z","value":"末尾改行なしにほんご","rows":1,"events":[{"t":571.1999999880791,"type":"selectionchange","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":573.7999999821186,"type":"compositionstart","data":"","cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":573.8999999761581,"type":"compositionupdate","data":"にほんご","cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":574.2999999821186,"type":"beforeinput","isComposing":true,"inputType":"insertCompositionText","data":"にほんご","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":6,"selEnd":6,"valueLen":6,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":574.6999999880791,"type":"input","isComposing":true,"inputType":"insertCompositionText","data":"にほんご","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":575.0999999940395,"type":"selectionchange","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":576.6999999880791,"type":"keydown","key":"z","code":"KeyZ","keyCode":90,"isComposing":true,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":576.6999999880791,"type":"beforeinput","isComposing":false,"inputType":"historyUndo","data":null,"cancelable":true,"defaultPrevented":true,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":578.1999999880791,"type":"keyup","key":"z","code":"KeyZ","keyCode":90,"isComposing":true,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":10,"selEnd":10,"valueLen":10,"vv":{"h":720,"top":0},"inner":{"h":720}}],"afterUndo":null}]
```

</details>

<details>
<summary>S-04</summary>

```json
{"limitation":"Synthetic events have no native default action; this measures handlers only.","results":[{"inputType":"insertLineBreak","cancelable":true,"before":{"value":"文章を読んでコメントを書きます。","rows":25},"keyPrevented":false,"beforeInputPrevented":false,"after":{"value":"文章を読んでコメントを書きます。","rows":25,"events":[{"t":274.7999999821186,"type":"keydown","key":"Unidentified","code":"","keyCode":229,"isComposing":false,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":16,"selEnd":16,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":275.59999999403954,"type":"beforeinput","isComposing":false,"inputType":"insertLineBreak","data":null,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":16,"selEnd":16,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}}]}},{"inputType":"insertLineBreak","cancelable":false,"before":{"value":"文章を読んでコメントを書きます。","rows":25},"keyPrevented":false,"beforeInputPrevented":false,"after":{"value":"文章を読んでコメントを書きます。","rows":25,"events":[{"t":391.2999999821186,"type":"keydown","key":"Unidentified","code":"","keyCode":229,"isComposing":false,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":16,"selEnd":16,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":391.59999999403954,"type":"beforeinput","isComposing":false,"inputType":"insertLineBreak","data":null,"cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":16,"selEnd":16,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":392,"type":"selectionchange","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":16,"selEnd":16,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}}]}},{"inputType":"deleteContentBackward","cancelable":true,"before":{"value":"文章を読んでコメントを書きます。","rows":25},"keyPrevented":false,"beforeInputPrevented":false,"after":{"value":"文章を読んでコメントを書きます。","rows":25,"events":[{"t":495.7999999821186,"type":"keydown","key":"Unidentified","code":"","keyCode":229,"isComposing":false,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":0,"selEnd":0,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":496,"type":"beforeinput","isComposing":false,"inputType":"deleteContentBackward","data":null,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":0,"selEnd":0,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":496.19999998807907,"type":"selectionchange","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":0,"selEnd":0,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}}]}},{"inputType":"deleteContentBackward","cancelable":false,"before":{"value":"文章を読んでコメントを書きます。","rows":25},"keyPrevented":false,"beforeInputPrevented":false,"after":{"value":"文章を読んでコメントを書きます。","rows":25,"events":[{"t":601.4000000059605,"type":"keydown","key":"Unidentified","code":"","keyCode":229,"isComposing":false,"cancelable":true,"defaultPrevented":false,"target":"inline-input","selStart":0,"selEnd":0,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":601.6999999880791,"type":"beforeinput","isComposing":false,"inputType":"deleteContentBackward","data":null,"cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":0,"selEnd":0,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}},{"t":602,"type":"selectionchange","cancelable":false,"defaultPrevented":false,"target":"inline-input","selStart":0,"selEnd":0,"valueLen":16,"vv":{"h":720,"top":0},"inner":{"h":720}}]}}]}
```

</details>

<details>
<summary>S-05</summary>

```json
{"opensImmediately":true,"selected":"文章を読んでコメントを書きます。","hasComposer":true,"savedRange":{"startLine":3,"startColumn":0,"endLine":3,"endColumn":16,"text":"文章を読んでコメントを書きます。","before":"# レビューの手引き\n\n","after":"\n指摘には理由と具体例を添えます。\n選択範囲を確認して保存します。\n日本語の文章"},"selectionAfterTap":""}
```

</details>

<details>
<summary>S-06</summary>

```json
{"activeClass":"inline-input","row":"L4","focusedInput":true}
```

</details>

<details>
<summary>S-07</summary>

```json
{"restart":{"eventOpens":2,"fileFetches":2,"displayed":"# changed while stopped","serverContent":"# changed while stopped\n"},"closed":{"body":"reviewer\nsample ▾\nラウンド 1 · 下書き中\n⚙ 設定\nレビューを提出\n📋 概要・全体コメント\n コメントあり\nguide.md\nguide.md\nAI指摘: すべて\n重大\n要修正\n軽微\n確認推奨\n却下済みも表示\nプレビュー\n折り返し\n編集\nファイルにコメント\n1\n# changed while stopped","projectStatus":200},"malformed":{"pageErrors":["Expected property name or '}' in JSON at position 1 (line 1 column 2)"]}}
```

</details>

<details>
<summary>S-08</summary>

```json
{"origin":"http://127.0.0.1:17777","cookies":[{"name":"reviewer_token_17777","sameSite":"Strict","httpOnly":true,"secure":false}],"crossSite":{"status":401,"cookieSent":false},"directStatus":200}
```

</details>

<details>
<summary>S-09</summary>

```json
{"browser":"153.0.8010.12","cpuRate":4,"viewport":{"width":412,"height":915},"results":[{"variant":"A","sample":1,"renderMs":657.9000000059605,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":1,"renderMs":642.6000000238419,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44},{"variant":"A","sample":2,"renderMs":630.6999999880791,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":2,"renderMs":669.7000000178814,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44},{"variant":"A","sample":3,"renderMs":695.0999999940395,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":3,"renderMs":656.6999999880791,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44},{"variant":"A","sample":4,"renderMs":628.6999999880791,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":4,"renderMs":625,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44},{"variant":"A","sample":5,"renderMs":635.9000000059605,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":5,"renderMs":700.1000000238419,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44}],"summaries":[{"variant":"A","renderMedianMs":635.9000000059605,"maxLongtaskMedianMs":0},{"variant":"B","renderMedianMs":656.6999999880791,"maxLongtaskMedianMs":0}],"ratio":1.0327095454975996,"provisional":"adopt"}
```

</details>

<details>
<summary>S-10</summary>

```json
{"resizeEvents":1,"bottom":395.5,"vvHeight":200,"vvTop":40,"visibleBottom":240,"obscured":true}
```

</details>

<details>
<summary>S-11</summary>

```json
{"written":{"body":"再起動後も残る試験用下書き","revision":7},"restored":{"body":"再起動後も残る試験用下書き","revision":7},"equal":true,"shutdown":"graceful context.close, not Android process kill"}
```

</details>

<details>
<summary>probes</summary>

```json
{"initial":{"t":227.89999997615814,"event":"load","widget":"resizes-visual","innerWidth":1280,"innerHeight":720,"visualViewport":{"width":1280,"height":720,"offsetTop":0,"scale":1},"heights":{"vh":720,"svh":720,"lvh":720,"dvh":720},"safeArea":{"top":0,"right":0,"bottom":0,"left":0},"media":{"hoverNone":{"query":"(hover: none)","matches":false},"pointerCoarse":{"query":"(pointer: coarse)","matches":false},"anyPointerCoarse":{"query":"(any-pointer: coarse)","matches":false},"anyHoverHover":{"query":"(any-hover: hover)","matches":true},"COMPACT":{"query":"(max-width: 599.98px)","matches":false},"MEDIUM":{"query":"(min-width: 600px) and (max-width: 839.98px)","matches":false},"NARROW":{"query":"(max-width: 839.98px)","matches":false},"TOUCH":{"query":"(hover: none) and (pointer: coarse)","matches":false}},"devicePixelRatio":1,"rem":16,"ua":"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/153.0.8010.12 Safari/537.36"},"consoleErrors":[],"blockRows":60,"editedRows":61,"undoRestored":true}
```

</details>

## C2 完了報告

対応範囲はPhase 0 C2と、統合仕様「Phase 0: 実機ゲート」の計測準備および自動検証です。C3以降へは進んでいません。製品の既存ファイルの変更はmain.tsxの初期化3行だけで、新規inputlog.tsを追加しています。

### 受け入れ条件

| 条件 | 結果 |
|---|---|
| S-01〜S-11の測定と記録 | ✓ 11項目を実測、計測ページ確認を含む12テスト成功 |
| デスクトップE2E | ✓ 21テスト成功、既存20件と入力ログ1件 |
| 入力ログの明示的な有効化 | ✓ 無効時非表示、1で表示、設定保持、0で解除、JSON内容とクリアを検証 |
| 無効時の登録なし | ✓ 登録関数の注入による単体テスト |
| リングバッファとdata制限 | ✓ 3000件、20コードポイント、全文フィールドの除外を検証 |
| 開発用probeの動作 | ✓ 2ページのconsole errorとpageerrorは0件 |
| distへのスパイク混入なし | ✓ ファイル名と内容検索で0件 |
| ランタイム依存の追加なし | ✓ package.jsonとpackage-lock.jsonの差分なし |

INV-1〜5はC1のE2Eで成功しました。INV-6はルーター、既存localStorageキー、APIの変更なしを差分で確認しました。追加キーは計測専用のreviewer.inputlogだけです。INV-7はGo全パッケージとVitest 68件が成功しました。意図的なデスクトップ挙動変更はありません。基準画像、計算済みスタイル、style.cssの差分は0です。

### 変更ファイル

`git status --short` の出力です。

```text
 D internal/server/dist/assets/c-D269Ak5_.js
 D internal/server/dist/assets/cpp-C0Q4q7a1.js
 D internal/server/dist/assets/css-BAKEo4JC.js
 D internal/server/dist/assets/dist-CIY70Sww.js
 D internal/server/dist/assets/graphql-x_W5n2e3.js
 D internal/server/dist/assets/html-F5D8QqEQ.js
 D internal/server/dist/assets/html-derivative-BNxlHdoH.js
 D internal/server/dist/assets/index-GqHxo-_a.js
 D internal/server/dist/assets/java-B1fifEpw.js
 D internal/server/dist/assets/jsx-Ca27TLWw.js
 D internal/server/dist/assets/lua-BkeX1blA.js
 D internal/server/dist/assets/php-Cxkq7N_8.js
 D internal/server/dist/assets/rst-DNa6nQau.js
 D internal/server/dist/assets/ruby-CfCHi0cj.js
 D internal/server/dist/assets/sql-DlCjuqWY.js
 D internal/server/dist/assets/tsx-rQh3dgVa.js
 D internal/server/dist/assets/typescript-CVGM08sB.js
 D internal/server/dist/assets/vue-DKrVYt3U.js
 D internal/server/dist/assets/xml-CNUthRhV.js
 M internal/server/dist/index.html
 M web/src/main.tsx
?? docs/mobile/
?? internal/server/dist/assets/c-DuXLUS11.js
?? internal/server/dist/assets/cpp-t6DK7xRf.js
?? internal/server/dist/assets/css-BpFy2_66.js
?? internal/server/dist/assets/dist-Ce7rw0Kr.js
?? internal/server/dist/assets/graphql-Druj-DAV.js
?? internal/server/dist/assets/html-DpJ5abjf.js
?? internal/server/dist/assets/html-derivative-D3mD3Zil.js
?? internal/server/dist/assets/index-D-r4uTsJ.js
?? internal/server/dist/assets/java-Cv9-APDJ.js
?? internal/server/dist/assets/jsx--iRKOmPq.js
?? internal/server/dist/assets/lua-CY3T7Uhw.js
?? internal/server/dist/assets/php-D2pFP3Oy.js
?? internal/server/dist/assets/rst-B-Ccx4J_.js
?? internal/server/dist/assets/ruby-I3cwlBc_.js
?? internal/server/dist/assets/sql-B6B1qd4A.js
?? internal/server/dist/assets/tsx-jbjZ8JpG.js
?? internal/server/dist/assets/typescript-DB-WZV5B.js
?? internal/server/dist/assets/vue-klFWY0gX.js
?? internal/server/dist/assets/xml-yOivx-N_.js
?? web/e2e/desktop/inputlog.e2e.ts
?? web/e2e/spike/
?? web/spike/
?? web/src/inputlog.test.ts
?? web/src/inputlog.ts
?? web/src/spike/
```

`git diff --stat` の出力です。未追跡の新規ファイルはこの集計に含まれません。distの19個の旧JSファイル削除と19個の新しいハッシュ名のJSファイルは、同じ再生成の差分です。

```text
 internal/server/dist/assets/c-D269Ak5_.js          |   1 -
 internal/server/dist/assets/cpp-C0Q4q7a1.js        |   1 -
 internal/server/dist/assets/css-BAKEo4JC.js        |   1 -
 internal/server/dist/assets/dist-CIY70Sww.js       |  63 ------
 internal/server/dist/assets/graphql-x_W5n2e3.js    |   1 -
 internal/server/dist/assets/html-F5D8QqEQ.js       |   1 -
 .../server/dist/assets/html-derivative-BNxlHdoH.js |   1 -
 internal/server/dist/assets/index-GqHxo-_a.js      | 252 ---------------------
 internal/server/dist/assets/java-B1fifEpw.js       |   1 -
 internal/server/dist/assets/jsx-Ca27TLWw.js        |   1 -
 internal/server/dist/assets/lua-BkeX1blA.js        |   1 -
 internal/server/dist/assets/php-Cxkq7N_8.js        |   1 -
 internal/server/dist/assets/rst-DNa6nQau.js        |   1 -
 internal/server/dist/assets/ruby-CfCHi0cj.js       |   1 -
 internal/server/dist/assets/sql-DlCjuqWY.js        |   1 -
 internal/server/dist/assets/tsx-rQh3dgVa.js        |   1 -
 internal/server/dist/assets/typescript-CVGM08sB.js |   1 -
 internal/server/dist/assets/vue-DKrVYt3U.js        |   1 -
 internal/server/dist/assets/xml-CNUthRhV.js        |   1 -
 internal/server/dist/index.html                    |   2 +-
 web/src/main.tsx                                   |   3 +
 21 files changed, 4 insertions(+), 333 deletions(-)
```

人間がステージする対象をファイル単位で列挙します。distの追加と削除も含みます。この作業ではステージしていません。

```text
internal/server/dist/assets/c-D269Ak5_.js
internal/server/dist/assets/cpp-C0Q4q7a1.js
internal/server/dist/assets/css-BAKEo4JC.js
internal/server/dist/assets/dist-CIY70Sww.js
internal/server/dist/assets/graphql-x_W5n2e3.js
internal/server/dist/assets/html-F5D8QqEQ.js
internal/server/dist/assets/html-derivative-BNxlHdoH.js
internal/server/dist/assets/index-GqHxo-_a.js
internal/server/dist/assets/java-B1fifEpw.js
internal/server/dist/assets/jsx-Ca27TLWw.js
internal/server/dist/assets/lua-BkeX1blA.js
internal/server/dist/assets/php-Cxkq7N_8.js
internal/server/dist/assets/rst-DNa6nQau.js
internal/server/dist/assets/ruby-CfCHi0cj.js
internal/server/dist/assets/sql-DlCjuqWY.js
internal/server/dist/assets/tsx-rQh3dgVa.js
internal/server/dist/assets/typescript-CVGM08sB.js
internal/server/dist/assets/vue-DKrVYt3U.js
internal/server/dist/assets/xml-CNUthRhV.js
internal/server/dist/index.html
web/src/main.tsx
docs/mobile/device-logs/.gitkeep
docs/mobile/phase0-device-report.md
docs/mobile/phase0-spike-report.md
internal/server/dist/assets/c-DuXLUS11.js
internal/server/dist/assets/cpp-t6DK7xRf.js
internal/server/dist/assets/css-BpFy2_66.js
internal/server/dist/assets/dist-Ce7rw0Kr.js
internal/server/dist/assets/graphql-Druj-DAV.js
internal/server/dist/assets/html-DpJ5abjf.js
internal/server/dist/assets/html-derivative-D3mD3Zil.js
internal/server/dist/assets/index-D-r4uTsJ.js
internal/server/dist/assets/java-Cv9-APDJ.js
internal/server/dist/assets/jsx--iRKOmPq.js
internal/server/dist/assets/lua-CY3T7Uhw.js
internal/server/dist/assets/php-D2pFP3Oy.js
internal/server/dist/assets/rst-B-Ccx4J_.js
internal/server/dist/assets/ruby-I3cwlBc_.js
internal/server/dist/assets/sql-B6B1qd4A.js
internal/server/dist/assets/tsx-jbjZ8JpG.js
internal/server/dist/assets/typescript-DB-WZV5B.js
internal/server/dist/assets/vue-klFWY0gX.js
internal/server/dist/assets/xml-yOivx-N_.js
web/e2e/desktop/inputlog.e2e.ts
web/e2e/spike/input.e2e.ts
web/e2e/spike/layout.e2e.ts
web/e2e/spike/network.e2e.ts
web/e2e/spike/perf-storage.e2e.ts
web/e2e/spike/probes.e2e.ts
web/e2e/spike/record.ts
web/spike/block-edit-probe.html
web/spike/viewport-probe.html
web/src/inputlog.test.ts
web/src/inputlog.ts
web/src/spike/block-edit-probe.ts
web/src/spike/viewport-probe.ts
```

### 検証コマンド

Webコマンドはwebディレクトリで実行しました。Goのキャッシュは許可された一時領域へ向け、WebビルドとGo検証は順番に実行しています。成功した最終実行の各出力の末尾20行を以下に載せます。全ログは `/tmp/reviewer-c2-validation/` にあります。

<details>
<summary><code>npm run typecheck</code>（終了コード0）</summary>

```text

> reviewer-web@0.0.0 typecheck
> tsc --noEmit

```

</details>

<details>
<summary><code>npm run typecheck:e2e</code>（終了コード0）</summary>

```text

> reviewer-web@0.0.0 typecheck:e2e
> tsc -p e2e/tsconfig.json --noEmit

```

</details>

<details>
<summary><code>npm test</code>（終了コード0）</summary>

```text

> reviewer-web@0.0.0 test
> vitest run


 RUN  v4.1.11 /home/ymotongpoo/repos/reviewer/web


 Test Files  3 passed (3)
      Tests  68 passed (68)
   Start at  02:04:11
   Duration  417ms (transform 234ms, setup 0ms, import 420ms, tests 67ms, environment 1ms)

```

</details>

<details>
<summary><code>npm run build</code>（終了コード0）</summary>

```text
../internal/server/dist/assets/html-DpJ5abjf.js                           57.30 kB │ gzip:  11.78 kB
../internal/server/dist/assets/markdown-BYOwaDjH.js                       59.32 kB │ gzip:   5.66 kB
../internal/server/dist/assets/python-gzcpVVnB.js                         69.94 kB │ gzip:   9.09 kB
../internal/server/dist/assets/c-DuXLUS11.js                              72.16 kB │ gzip:  10.54 kB
../internal/server/dist/assets/swift-CyEgAFGc.js                          87.22 kB │ gzip:  14.79 kB
../internal/server/dist/assets/latex-ZgXIZN55.js                          89.15 kB │ gzip:  10.96 kB
../internal/server/dist/assets/csharp-oqKa8noW.js                         90.18 kB │ gzip:  10.71 kB
../internal/server/dist/assets/php-D2pFP3Oy.js                           113.08 kB │ gzip:  28.85 kB
../internal/server/dist/assets/asciidoc-SCjQUq34.js                      136.04 kB │ gzip:   9.45 kB
../internal/server/dist/assets/mdx-DQZ5AkYe.js                           136.10 kB │ gzip:  23.54 kB
../internal/server/dist/assets/javascript-BgS3c2Ky.js                    174.82 kB │ gzip:  16.62 kB
../internal/server/dist/assets/tsx-jbjZ8JpG.js                           175.59 kB │ gzip:  16.67 kB
../internal/server/dist/assets/jsx--iRKOmPq.js                           177.84 kB │ gzip:  16.76 kB
../internal/server/dist/assets/typescript-DB-WZV5B.js                    181.13 kB │ gzip:  16.28 kB
../internal/server/dist/assets/katex-DnD4SCnU.js                         258.89 kB │ gzip:  77.66 kB
../internal/server/dist/assets/dist-Ce7rw0Kr.js                          297.38 kB │ gzip: 116.19 kB
../internal/server/dist/assets/index-D-r4uTsJ.js                         410.22 kB │ gzip: 135.66 kB
../internal/server/dist/assets/cpp-t6DK7xRf.js                           796.96 kB │ gzip:  60.65 kB

✓ built in 483ms
```

</details>

<details>
<summary><code>GOCACHE=/tmp/reviewer-e2e-go-cache GOMODCACHE=/tmp/reviewer-e2e-go-mod go test ./...</code>（終了コード0）</summary>

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

</details>

<details>
<summary><code>GOCACHE=/tmp/reviewer-e2e-go-cache GOMODCACHE=/tmp/reviewer-e2e-go-mod go build -o bin/reviewer ./cmd/reviewer</code>（終了コード0）</summary>

```text
（出力なし）
```

</details>

<details>
<summary><code>E2E_SKIP_BUILD=1 npm run e2e:desktop</code>（終了コード0）</summary>

```text
  ✓   4 [desktop] › e2e/desktop/edit.e2e.ts:8:3 › save preserves original bytes and line endings: noeol.md (738ms)
  ✓   5 [desktop] › e2e/desktop/edit.e2e.ts:8:3 › save preserves original bytes and line endings: mixed.md (652ms)
  ✓   6 [desktop] › e2e/desktop/edit.e2e.ts:24:1 › 409 preserves draft and external file (4.9s)
  ✓   7 [desktop] › e2e/desktop/edit.e2e.ts:41:1 › one character renders at most two rows (INV-5) (2.8s)
  ✓   8 [desktop] › e2e/desktop/edit.e2e.ts:57:1 › CDP Japanese composition commits once and one Undo restores text (574ms)
  ✓   9 [desktop] › e2e/desktop/inputlog.e2e.ts:4:1 › input log is opt-in, persists the flag, exports metadata and can be disabled (1.0s)
  ✓  10 [desktop] › e2e/desktop/interactions.e2e.ts:7:1 › gutter click and Shift-click extend a line comment (2.3s)
  ✓  11 [desktop] › e2e/desktop/interactions.e2e.ts:25:1 › gutter drag selects L3 through L6 (1.6s)
  ✓  12 [desktop] › e2e/desktop/interactions.e2e.ts:43:3 › selected text becomes a character range: guide.md (1.4s)
  ✓  13 [desktop] › e2e/desktop/interactions.e2e.ts:43:3 › selected text becomes a character range: emoji.md (1.4s)
  ✓  14 [desktop] › e2e/desktop/interactions.e2e.ts:69:1 › hover controls, double click, Ctrl+S, Escape and i (689ms)
  ✓  15 [desktop] › e2e/desktop/interactions.e2e.ts:93:1 › autosave after debounce, Ctrl+Enter, submit counts and tree navigation (1.6s)
  ✓  16 [desktop] › e2e/desktop/interactions.e2e.ts:111:1 › external file write arrives over SSE (835ms)
  ✓  17 [desktop] › e2e/desktop/styles.e2e.ts:6:1 › computed desktop styles including hovered controls (1.7s)
  ✓  18 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 280 (5.4s)
  ✓  19 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1024x768 sidebar 280 (4.4s)
  ✓  20 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 840x900 sidebar 280 (4.2s)
  ✓  21 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 400 (6.0s)

  21 passed (47.5s)
```

</details>

<details>
<summary><code>E2E_SKIP_BUILD=1 npm run e2e:spike</code>（終了コード0）</summary>

```text
S-02: [{"name":"mobile-touch","before":{"(hover: none)":true,"(hover: hover)":false,"(pointer: coarse)":true,"(pointer: fine)":false,"(any-pointer: coarse)":true,"(any-hover: hover)":false},"afterCDP":{"(hover: none)":true,"(hover: hover)":false,"(pointer: coarse)":true,"(pointer: fine)":false,"(any-pointer: coarse)":true,"(any-hover: hover)":false}},{"name":"touch-only","before":{"(hover: none)":true,"(hover: hover)":false,"(pointer: coarse)":true,"(pointer: fine)":false,"(any-pointer: coarse)":true,"(any-hover: hover)":false},"afterCDP":{"(hover: none)":true,"(hover: hover)":false,"(pointer: coarse)":true,"(pointer: fine)":false,"(any-pointer: coarse)":true,"(any-hover: hover)":false}},{"name":"desktop","before":{"(hover: none)":false,"(hover: hover)":true,"(pointer: coarse)":false,"(pointer: fine)":true,"(any-pointer: coarse)":false,"(any-hover: hover)":true},"afterCDP":{"(hover: none)":false,"(hover: hover)":true,"(pointer: coarse)":false,"(pointer: fine)":true,"(any-pointer: coarse)":false,"(any-hover: hover)":true}}]
  ✓   4 [spike] › e2e/spike/layout.e2e.ts:47:1 › S-02 media emulation and CDP feature overrides (444ms)
S-05: {"opensImmediately":true,"selected":"文章を読んでコメントを書きます。","hasComposer":true,"savedRange":{"startLine":3,"startColumn":0,"endLine":3,"endColumn":16,"text":"文章を読んでコメントを書きます。","before":"# レビューの手引き\n\n","after":"\n指摘には理由と具体例を添えます。\n選択範囲を確認して保存します。\n日本語の文章"},"selectionAfterTap":""}
  ✓   5 [spike] › e2e/spike/layout.e2e.ts:70:1 › S-05 touch gutter and DOM character selection (1.7s)
S-06: {"activeClass":"inline-input","row":"L4","focusedInput":true}
  ✓   6 [spike] › e2e/spike/layout.e2e.ts:99:1 › S-06 focus after tapping another row during editing (720ms)
S-10: {"resizeEvents":1,"bottom":395.5,"vvHeight":200,"vvTop":40,"visibleBottom":240,"obscured":true}
  ✓   7 [spike] › e2e/spike/layout.e2e.ts:114:1 › S-10 synthetic visualViewport resize and current composer overlap (748ms)
S-07: {"restart":{"eventOpens":2,"fileFetches":2,"displayed":"# changed while stopped","serverContent":"# changed while stopped\n"},"closed":{"body":"reviewer\nsample ▾\nラウンド 1 · 下書き中\n⚙ 設定\nレビューを提出\n📋 概要・全体コメント\n コメントあり\nguide.md\nguide.md\nAI指摘: すべて\n重大\n要修正\n軽微\n確認推奨\n却下済みも表示\nプレビュー\n折り返し\n編集\nファイルにコメント\n1\n# changed while stopped","projectStatus":200},"malformed":{"pageErrors":["Expected property name or '}' in JSON at position 1 (line 1 column 2)"]}}
  ✓   8 [spike] › e2e/spike/network.e2e.ts:9:1 › S-07 restart, project close and malformed SSE (11.8s)
S-08: {"origin":"http://127.0.0.1:17777","cookies":[{"name":"reviewer_token_17777","sameSite":"Strict","httpOnly":true,"secure":false}],"crossSite":{"status":401,"cookieSent":false},"directStatus":200}
  ✓   9 [spike] › e2e/spike/network.e2e.ts:54:1 › S-08 Strict cookie on cross-site navigation (692ms)
S-09: {"browser":"153.0.8010.12","cpuRate":4,"viewport":{"width":412,"height":915},"results":[{"variant":"A","sample":1,"renderMs":657.9000000059605,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":1,"renderMs":642.6000000238419,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44},{"variant":"A","sample":2,"renderMs":630.6999999880791,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":2,"renderMs":669.7000000178814,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44},{"variant":"A","sample":3,"renderMs":695.0999999940395,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":3,"renderMs":656.6999999880791,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44},{"variant":"A","sample":4,"renderMs":628.6999999880791,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":4,"renderMs":625,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44},{"variant":"A","sample":5,"renderMs":635.9000000059605,"maxLongtaskMs":0,"longtasks":[],"positions":[5982,11964,17946,23928,29910,35892,41874,47856,53838,59820],"scrollHeight":60532,"rowHeight":20},{"variant":"B","sample":5,"renderMs":700.1000000238419,"maxLongtaskMs":0,"longtasks":[],"positions":[13182,26364,39546,52728,65910,79092,92274,105456,118638,131820],"scrollHeight":132532,"rowHeight":44}],"summaries":[{"variant":"A","renderMedianMs":635.9000000059605,"maxLongtaskMedianMs":0},{"variant":"B","renderMedianMs":656.6999999880791,"maxLongtaskMedianMs":0}],"ratio":1.0327095454975996,"provisional":"adopt"}
  ✓  10 [spike] › e2e/spike/perf-storage.e2e.ts:8:1 › S-09 3000 rows at 4x CPU, five samples per CSS variant (1.2m)
S-11: {"written":{"body":"再起動後も残る試験用下書き","revision":7},"restored":{"body":"再起動後も残る試験用下書き","revision":7},"equal":true,"shutdown":"graceful context.close, not Android process kill"}
  ✓  11 [spike] › e2e/spike/perf-storage.e2e.ts:62:1 › S-11 IndexedDB survives persistent browser context restart (1.0s)
probes: {"initial":{"t":227.89999997615814,"event":"load","widget":"resizes-visual","innerWidth":1280,"innerHeight":720,"visualViewport":{"width":1280,"height":720,"offsetTop":0,"scale":1},"heights":{"vh":720,"svh":720,"lvh":720,"dvh":720},"safeArea":{"top":0,"right":0,"bottom":0,"left":0},"media":{"hoverNone":{"query":"(hover: none)","matches":false},"pointerCoarse":{"query":"(pointer: coarse)","matches":false},"anyPointerCoarse":{"query":"(any-pointer: coarse)","matches":false},"anyHoverHover":{"query":"(any-hover: hover)","matches":true},"COMPACT":{"query":"(max-width: 599.98px)","matches":false},"MEDIUM":{"query":"(min-width: 600px) and (max-width: 839.98px)","matches":false},"NARROW":{"query":"(max-width: 839.98px)","matches":false},"TOUCH":{"query":"(hover: none) and (pointer: coarse)","matches":false}},"devicePixelRatio":1,"rem":16,"ua":"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/153.0.8010.12 Safari/537.36"},"consoleErrors":[],"blockRows":60,"editedRows":61,"undoRestored":true}
  ✓  12 [spike] › e2e/spike/probes.e2e.ts:12:1 › development probes load without console errors and support measurement/edit/undo (1.1s)

  12 passed (1.7m)
```

</details>

次の確認も成功しました。

```sh
rg -l 'block-edit-probe|viewport-probe' internal/server/dist
# 出力0件、rgの終了コード1（該当なし）
git diff --check
# 出力なし、終了コード0
git diff --stat -- web/src/style.css web/package.json web/package-lock.json 'web/e2e/desktop/*-snapshots/'
# 出力なし
git diff --cached --stat
# 出力なし（ステージ済み変更なし）
```

加えてdist配下のファイル名にspikeやprobeがないことを検査しました。提案するコミットメッセージは `chore: add mobile phase 0 probes and spike report` です。コミット、push、デプロイ、systemd操作は実行していません。
