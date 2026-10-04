# The text and the images of a PDF, page by page (ADR-0064, regla 10; ADR-0067). pypdf, pure Python and BSD (ADR-0071).

import io
import logging
from dataclasses import dataclass

from PIL import Image
from pypdf import PdfReader
from pypdf.errors import PyPdfError

logger = logging.getLogger("sonia.worker.pdf")

# The formats the three providers' vision models take as they are; anything else is converted before it is sent.
_SENDABLE = {"JPEG": "image/jpeg", "PNG": "image/png", "GIF": "image/gif", "WEBP": "image/webp"}


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


# read never fails on one image: an image that cannot be decoded is left out and logged, and the page keeps its text (ADR-0067,
# regla 9). It fails on the document, when pypdf cannot open it at all.
def read(data: bytes, *, max_side: int) -> list[Page]:
    try:
        reader = PdfReader(io.BytesIO(data))
        pages = list(reader.pages)
    except (PyPdfError, ValueError, OSError) as err:
        raise UnreadablePDF(f"the PDF cannot be opened: {type(err).__name__}: {err}") from err

    out: list[Page] = []
    for number, page in enumerate(pages, start=1):
        try:
            # PostgreSQL refuses a NUL in text, and some fonts make pypdf return one: it goes here, before the chunking, so what is
            # embedded and what document_chunks stores are still the same text.
            text = (page.extract_text() or "").replace("\x00", "")
        except Exception as err:  # noqa: BLE001 - one page pypdf cannot read must not cost the document its other pages
            logger.warning("page text not extracted", extra={"context": {"page": number, "error": str(err)}})
            text = ""
        out.append(Page(number=number, text=text, images=tuple(_images(page, number, max_side))))
    return out


def _images(page: object, number: int, max_side: int) -> list[PageImage]:
    found: list[PageImage] = []
    try:
        files = list(getattr(page, "images", []))
    except Exception as err:  # noqa: BLE001 - an image table pypdf cannot walk leaves the page with its text
        logger.warning("page images not listed", extra={"context": {"page": number, "error": str(err)}})
        return found
    for file in files:
        try:
            found.append(sendable(file.image, max_side=max_side))
        except Exception as err:  # noqa: BLE001 - one image that cannot be decoded is left out (ADR-0067, regla 9)
            logger.warning("image not decoded", extra={"context": {"page": number, "error": str(err)}})
    return found


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
