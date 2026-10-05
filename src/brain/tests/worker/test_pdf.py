# ADR-0064, regla 10 and ADR-0067: the text and the images of each page, and one bad image never costs the page its text.

import io

import pytest
from PIL import Image
from pypdf import PageObject

from tests.worker.pdfs import image_pdf, joined, text_pdf
from worker.pdf import PageImage, UnreadablePDF, read, sendable


def test_each_page_keeps_its_text_and_its_images() -> None:
    document = joined(text_pdf(["Depto 2 ambientes en Jesus Maria", "Requisitos para alquilar"]), image_pdf(300, 200))

    pages = read(document, max_side=1568)

    assert [p.number for p in pages] == [1, 2, 3]
    assert "Jesus Maria" in pages[0].text and "Requisitos" in pages[1].text
    assert pages[0].images == () and pages[2].text.strip() == ""
    (image,) = pages[2].images
    assert (image.width, image.height) == (300, 200)
    assert image.media_type in {"image/jpeg", "image/png"}
    assert Image.open(io.BytesIO(image.data)).size == (300, 200)


def test_a_nul_in_the_text_is_dropped_before_postgresql_sees_it(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(PageObject, "extract_text", lambda self, *args, **kwargs: "Depto\x00 2 ambientes\x00")

    (page,) = read(text_pdf(["Depto 2 ambientes"]), max_side=1568)

    assert page.text == "Depto 2 ambientes"


# ADR-0076: the layout mode keeps the lines and the blank line between paragraphs, and drops the padding that places them.
def test_the_layout_mode_keeps_the_lines_and_drops_the_padding(monkeypatch: pytest.MonkeyPatch) -> None:
    def extract(self: PageObject, *args: object, extraction_mode: str = "plain", **kwargs: object) -> str:
        if extraction_mode == "layout":
            return "      Depto   2   ambientes   \n\n   en  Jesus Maria\t\t\n"
        return "Depto\n \n2\n \nambientes"

    monkeypatch.setattr(PageObject, "extract_text", extract)

    (layout,) = read(text_pdf(["x"]), max_side=1568, mode="layout")
    (plain,) = read(text_pdf(["x"]), max_side=1568)

    assert layout.text == "Depto 2 ambientes\n\nen Jesus Maria\n"
    assert plain.text == "Depto\n \n2\n \nambientes", "plain is still pypdf's text as it comes"


def test_the_layout_mode_reads_a_real_pdf_line_by_line() -> None:
    (page,) = read(text_pdf(["Depto 2 ambientes\nRequisitos para alquilar"]), max_side=1568, mode="layout")
    assert page.text.strip() == "Depto 2 ambientes\nRequisitos para alquilar"


def test_a_page_the_layout_mode_cannot_read_is_read_plain(monkeypatch: pytest.MonkeyPatch) -> None:
    def extract(self: PageObject, *args: object, extraction_mode: str = "plain", **kwargs: object) -> str:
        if extraction_mode == "layout":
            raise ZeroDivisionError("a font without widths")
        return "Depto 2 ambientes"

    monkeypatch.setattr(PageObject, "extract_text", extract)

    (page,) = read(text_pdf(["x"]), max_side=1568, mode="layout")

    assert page.text == "Depto 2 ambientes"


def test_what_is_not_a_pdf_is_refused_as_unreadable() -> None:
    with pytest.raises(UnreadablePDF):
        read(b"%PDF-1.7 esto no tiene objetos", max_side=1568)


# A large image is scaled before it is sent, and keeps the size it had, which is what the minimum of ADR-0067 is judged on.
def test_a_large_image_is_scaled_down_and_keeps_its_original_size() -> None:
    big = Image.new("RGB", (4000, 1000), (10, 20, 30))
    big.format = "JPEG"
    image = sendable(big, max_side=1568)
    assert (image.width, image.height) == (4000, 1000)
    assert max(Image.open(io.BytesIO(image.data)).size) == 1568

    transparent = Image.new("RGBA", (50, 50))
    transparent.format = "TIFF"
    assert sendable(transparent, max_side=1568).media_type == "image/png"
    gray = Image.new("L", (50, 50))
    assert sendable(gray, max_side=1568).media_type == "image/jpeg"


# ADR-0067, regla 6, before decoding: an image whose declared size is under the minimum never reaches sendable().
def test_an_image_declared_under_the_minimum_is_not_even_decoded(monkeypatch: pytest.MonkeyPatch) -> None:
    import worker.pdf as pdf

    decoded: list[tuple[int, int]] = []
    real = pdf.sendable

    def counting(image: Image.Image, *, max_side: int) -> PageImage:
        decoded.append(image.size)
        return real(image, max_side=max_side)

    monkeypatch.setattr(pdf, "sendable", counting)
    pages = read(joined(image_pdf(60, 40), image_pdf(300, 200)), max_side=1568, min_width=120, min_height=120)

    assert pages[0].images == () and len(pages[1].images) == 1
    assert decoded == [(300, 200)], "the logo was decoded only to be thrown away"


def test_an_image_that_cannot_be_decoded_is_left_out_and_the_page_keeps_its_text(monkeypatch: pytest.MonkeyPatch) -> None:
    import worker.pdf as pdf

    def broken(image: object, *, max_side: int) -> object:
        raise OSError("not an image")

    monkeypatch.setattr(pdf, "sendable", broken)
    pages = read(joined(text_pdf(["texto"]), image_pdf(300, 300)), max_side=1568)
    assert pages[0].text.strip() == "texto" and pages[1].images == ()
