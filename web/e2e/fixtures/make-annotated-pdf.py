"""Regenerate the synthetic PDF fixture: uv run --with pymupdf this_file.py."""
from pathlib import Path
import pymupdf

doc = pymupdf.open()
page = doc.new_page()
for y, text in [
    (100, "The pipeline exports telemetry."),
    (140, "Multiple lines of text"),
    (156, "continue on the next line."),
    (200, "Repeated phrase."),
    (240, "This text was removed."),
]:
    page.insert_text((72, y), text, fontsize=12)

def comment(page, targets, body):
    quads = [q for target in targets for q in page.search_for(target, quads=True)]
    assert quads, targets
    annot = page.add_highlight_annot(quads)
    annot.set_info(content=body)
    annot.update()

comment(page, ["exports telemetry"], "送信先を明示してください。")
comment(page, ["Multiple lines of text", "continue on the next line."], "説明を短くしてください。")
comment(page, ["Repeated phrase."], "どちらの章かを確認してください。")
comment(page, ["This text was removed."], "削除済みの文章への指摘です。")
page.add_text_annot((400, 100), "付箋は対象外")
strike = page.add_strikeout_annot(page.search_for("pipeline"))
strike.set_info(content="取り消し線は対象外")
strike.update()
page.add_highlight_annot(page.search_for("The"))  # no comment

page = doc.new_page()
page.insert_text((72, 100), "日本語の文章を確認します。", fontname="japan", fontsize=12)
comment(page, ["日本語の文章"], "用語の説明を補足してください。")
doc.save(Path(__file__).with_name("annotated.pdf"))
