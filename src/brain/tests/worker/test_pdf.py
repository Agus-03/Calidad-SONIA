# ADR-0064, regla 10 and ADR-0067: the text and the images of each page, and one bad image never costs the page its text.

import io

import pytest
from PIL import Image
from pypdf import PageObject

from tests.worker.pdfs import image_pdf, joined, text_pdf
from worker.pdf import UnreadablePDF, read, sendable


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


def test_an_image_that_cannot_be_decoded_is_left_out_and_the_page_keeps_its_text(monkeypatch: pytest.MonkeyPatch) -> None:
    import worker.pdf as pdf

    def broken(image: object, *, max_side: int) -> object:
        raise OSError("not an image")

    monkeypatch.setattr(pdf, "sendable", broken)
    pages = read(joined(text_pdf(["texto"]), image_pdf(300, 300)), max_side=1568)
    assert pages[0].text.strip() == "texto" and pages[1].images == ()
