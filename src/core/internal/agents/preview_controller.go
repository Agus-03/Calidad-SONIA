package agents

import (
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The body of a preview: every block on screen, each with the three fields a draft has (ADR-0040).
const fieldComponents = "components"

var draftFields = []string{fieldType, fieldContent, fieldPosition}

type PreviewController struct {
	service *Previews
	log     *slog.Logger
}

func NewPreviewController(service *Previews, log *slog.Logger) *PreviewController {
	if log == nil {
		log = slog.Default()
	}
	return &PreviewController{service: service, log: log}
}

type previewResponse struct {
	System       string   `json:"system"`
	RulesVersion int      `json:"rules_version"`
	Components   []string `json:"components"`
}

func (c *PreviewController) preview(gc *gin.Context) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return
	}
	agentID, err := agentIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	drafts, err := readDrafts(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	shown, err := c.service.Assemble(gc.Request.Context(), actor.AccountID, agentID, drafts)
	if err != nil {
		soniahttp.Fail(gc, c.log, translatePreview(err))
		return
	}
	types := make([]string, 0, len(shown.Components))
	for _, t := range shown.Components {
		types = append(types, string(t))
	}
	soniahttp.OK(gc, previewResponse{System: shown.System, RulesVersion: shown.RulesVersion, Components: types})
}

// readDrafts reads each draft by its keys, like a component's body: a field a draft does not have is refused by name.
func readDrafts(gc *gin.Context) ([]Draft, error) {
	raw, err := body(gc, []string{fieldComponents})
	if err != nil {
		return nil, err
	}
	var items []map[string]json.RawMessage
	value, sent := raw[fieldComponents]
	if !sent || string(value) == "null" {
		return nil, soniahttp.BadRequest("invalid_body", "components is required, and an empty list is a valid one")
	}
	if err := json.Unmarshal(value, &items); err != nil {
		return nil, soniahttp.BadRequest("invalid_body", "components has to be a list of objects").WithCause(err)
	}

	drafts := make([]Draft, 0, len(items))
	for _, item := range items {
		for field := range item {
			known := false
			for _, a := range draftFields {
				known = known || a == field
			}
			if !known {
				return nil, soniahttp.BadRequest("unknown_field", "the field "+field+" is not part of a draft")
			}
		}
		var d Draft
		var kind string
		for _, err := range []error{
			required(item, fieldType, &kind),
			required(item, fieldContent, &d.Content),
			required(item, fieldPosition, &d.Position),
		} {
			if err != nil {
				return nil, err
			}
		}
		d.Type = enums.PromptComponentType(kind)
		drafts = append(drafts, d)
	}
	return drafts, nil
}

// translatePreview is translateComponent plus what only a preview can say.
func translatePreview(err error) error {
	switch {
	case errors.Is(err, ErrDuplicateDraftType):
		return soniahttp.BadRequest("duplicate_component_type", "two drafts of one type: the panel writes one of each").WithCause(err)
	case errors.Is(err, ErrDuplicateDraftPosition):
		return soniahttp.BadRequest("duplicate_component_position", "two drafts at one position").WithCause(err)
	case errors.Is(err, ErrPreviewUnavailable):
		return soniahttp.Unavailable("preview_unavailable", "the brain could not assemble the preview, try again").WithCause(err)
	default:
		return translateComponent(err)
	}
}
