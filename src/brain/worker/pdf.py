# The text and the images of a PDF, page by page (ADR-0064, regla 10; ADR-0067). pypdf, pure Python and BSD (ADR-0071).

import io
import logging
import re
from dataclasses import dataclass

from PIL import Image
from pypdf import PdfReader
from pypdf.errors import PyPdfError

from knowledge.chunking import PdfText

logger = logging.getLogger("sonia.worker.pdf")

# The formats the three providers' vision models take as they are; anything else is converted before it is sent.
_SENDABLE = {"JPEG": "image/jpeg", "PNG": "image/png", "GIF": "image/gif", "WEBP": "image/webp"}

# The padding pypdf's layout mode writes to put a line at its column on the page: any tab, and any run of spaces (ADR-0076,
# regla 2).
_PADDING = re.compile(r"[ \t]{2,}|\t")


class UnreadablePDF(Exception):
    pass


@dataclass(frozen=True)
class PageImage:
    media_type: str
    data: bytes
    width: int
    height: int


@dataclass(frozen=True)
class Page:
    number: int
    text: str
    images: tuple[PageImage, ...]
    # Asked for in layout mode and read plain instead (ADR-0076, regla 4), so the ingestion can say how many in its log.
    read_plain: bool = False


# read never fails on one image: an image that cannot be decoded is left out and logged, and the page keeps its text (ADR-0067,
# regla 9). It fails on the document, when pypdf cannot open it at all. min_width and min_height leave out, before decoding them,
# the images whose declared size is under the minimum of ADR-0067, regla 6: a logo repeated on every page is never decoded.
# mode and layout_min_ratio are pdf_text and layout_min_ratio of rules/chunking.yaml (ADR-0076), and have no default: a caller
# that forgot them would extract one version's text and record it under another.
def read(
    data: bytes,
    *,
    max_side: int,
    mode: PdfText,
    layout_min_ratio: float,
    min_width: int = 1,
    min_height: int = 1,
) -> list[Page]:
    try:
        reader = PdfReader(io.BytesIO(data))
        pages = list(reader.pages)
    except (PyPdfError, ValueError, OSError) as err:
        raise UnreadablePDF(f"the PDF cannot be opened: {type(err).__name__}: {err}") from err

    out: list[Page] = []
    for number, page in enumerate(pages, start=1):
        try:
            text, read_plain = _text(page, number, mode, layout_min_ratio)
            # PostgreSQL refuses a NUL in text, and some fonts make pypdf return one: it goes here, before the chunking, so what is
            # embedded and what document_chunks stores are still the same text.
            text = text.replace("\x00", "")
        except Exception as err:  # noqa: BLE001 - one page pypdf cannot read must not cost the document its other pages
            logger.warning("page text not extracted", extra={"context": {"page": number, "error": str(err)}})
            text, read_plain = "", False
        images = tuple(_images(page, number, max_side, min_width, min_height))
        out.append(Page(number=number, text=text, images=images, read_plain=read_plain))
    return out


# ADR-0076. In layout mode the padding goes and the line breaks stay: runs of spaces become one and each line loses its margins,
# which are not text and would count as characters in the estimate of ADR-0071, while the blank lines of a vertical gap are what
# the separators read as the end of a paragraph (regla 2). The layout mode loses text without raising on some pages (a page
# drawn inside a form XObject, rotated text, a font it cannot read), so the page is read both ways and keeps the layout text
# only when it holds at least layout_min_ratio of the plain text's characters; otherwise, or when the layout mode raises, it is
# read plain (regla 4). Answers the text and whether it was read plain.
def _text(page: object, number: int, mode: PdfText, layout_min_ratio: float) -> tuple[str, bool]:
    if mode == "plain":
        return page.extract_text() or "", False  # type: ignore[attr-defined]
    try:
        laid_out = page.extract_text(extraction_mode="layout") or ""  # type: ignore[attr-defined]
    except Exception as err:  # noqa: BLE001 - the layout mode fails on pages the plain one reads, and the page keeps its text
        logger.warning("page not read in layout mode", extra={"context": {"page": number, "error": str(err)}})
        return page.extract_text() or "", True  # type: ignore[attr-defined]
    laid_out = "\n".join(_PADDING.sub(" ", line).strip() for line in laid_out.split("\n"))
    try:
        plain = page.extract_text() or ""  # type: ignore[attr-defined]
    except Exception:  # noqa: BLE001 - plain is only the yardstick here, and the layout mode already read the page
        return laid_out, False
    if _characters(laid_out) >= layout_min_ratio * _characters(plain):
        return laid_out, False
    return plain, True


# What the two modes are compared on: the characters that are not white space, which is the only thing they should agree on.
def _characters(text: str) -> int:
    return sum(not c.isspace() for c in text)


def _images(page: object, number: int, max_side: int, min_width: int, min_height: int) -> list[PageImage]:
    found: list[PageImage] = []
    try:
        images = page.images  # type: ignore[attr-defined]
        keys = list(images.keys())
    except Exception as err:  # noqa: BLE001 - an image table pypdf cannot walk leaves the page with its text
        logger.warning("page images not listed", extra={"context": {"page": number, "error": str(err)}})
        return found
    for key in keys:
        declared = _declared_size(page, key)
        if declared is not None and (declared[0] < min_width or declared[1] < min_height):
            continue
        try:
            # Decoded here and not before: pypdf builds the image when it is asked for it.
            found.append(sendable(images[key].image, max_side=max_side))
        except Exception as err:  # noqa: BLE001 - one image that cannot be decoded is left out (ADR-0067, regla 9)
            logger.warning("image not decoded", extra={"context": {"page": number, "error": str(err)}})
    return found


# The size an image XObject declares, read from its dictionary without decoding it. None when there is no such dictionary to read
# (an inline image, or one inside a form): that image is decoded and judged by its real size afterwards, as before.
def _declared_size(page: object, key: object) -> tuple[int, int] | None:
    if not isinstance(key, str) or key.startswith("~"):
        return None
    try:
        xobject = page["/Resources"]["/XObject"][key].get_object()  # type: ignore[index]
        return int(xobject["/Width"]), int(xobject["/Height"])
    except Exception:  # noqa: BLE001 - a dictionary that does not say its size is decoded and measured instead
        return None


# The image as a provider can take it: its own bytes when the format is one they read and it is not too large, and otherwise
# scaled to max_side on the long side and re-encoded. The size kept is the original one, which is what the minimum is judged on.
def sendable(image: Image.Image, *, max_side: int) -> PageImage:
    width, height = image.size
    media_type = _SENDABLE.get(image.format or "")
    if media_type is not None and max(width, height) <= max_side:
        buffer = io.BytesIO()
        image.save(buffer, format=image.format)
        return PageImage(media_type=media_type, data=buffer.getvalue(), width=width, height=height)

    copy = image.copy()
    copy.thumbnail((max_side, max_side))
    # PNG keeps a transparency a JPEG would paint black; a photograph without one is far smaller as JPEG.
    if copy.mode in ("RGBA", "LA", "P"):
        target, media_type = "PNG", "image/png"
    else:
        copy = copy.convert("RGB")
        target, media_type = "JPEG", "image/jpeg"
    buffer = io.BytesIO()
    copy.save(buffer, format=target)
    return PageImage(media_type=media_type, data=buffer.getvalue(), width=width, height=height)
