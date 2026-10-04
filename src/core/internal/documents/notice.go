package documents

// NoticeVersion is what documents.personal_data_terms_version stores. A change to NoticeText that changes what is accepted moves
// it, so every document keeps the proof of which text was accepted for it (ADR-0066, reglas 2 y 3).
const NoticeVersion = 1

// NoticeText is ADR-0066, regla 1, in the words the admin reads before uploading. It lives in the core because the core is the
// one that records the acceptance. It names the providers, as ADR-0067 and ADR-0069 ask.
const NoticeText = "El documento puede contener datos personales. Al subirlo, su contenido se guarda cifrado en Google " +
	"(Cloud Storage for Firebase), se envía a Voyage AI para convertirlo en vectores, a Anthropic para describir sus " +
	"imágenes y, cuando el agente responde con él, " +
	"al proveedor del modelo de lenguaje de ese agente. Quien lo sube se responsabiliza por haberlo cargado."
