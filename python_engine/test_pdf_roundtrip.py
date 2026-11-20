from pdf import PDFEngine


def test_create_merge_split_and_extract_preserve_page_order(tmp_path):
    engine = PDFEngine()
    first = str(tmp_path / "first.pdf")
    second = str(tmp_path / "second.pdf")
    merged = str(tmp_path / "merged.pdf")
    engine.create_pdf(first, text="First audit page", title="First")
    engine.create_pdf(second, text="Second audit page", title="Second")
    assert engine.merge_pdfs([first, second], merged)["source_count"] == 2
    pages = engine.extract_text(merged)["pages"]
    assert len(pages) == 2
    assert "First audit page" in pages[0]["text"]
    assert "Second audit page" in pages[1]["text"]
    split = engine.split_pdf(merged, str(tmp_path / "split"), pages=[2])
    assert split["count"] == 1
    assert "Second audit page" in engine.extract_text(split["files"][0])["pages"][0]["text"]
