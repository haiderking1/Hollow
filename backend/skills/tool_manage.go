package skills

import (
	"encoding/json"
	"fmt"

	"github.com/enough/enough/backend/approval"
)

type SkillManageOptions struct {
	GuardEnabled       bool
	MarkCreatedAsAgent bool
	// ArchiveOnDelete turns 'delete' into an archive (move to .archive/).
	// Set for background-review and curator forks — autonomous passes must
	// never destroy data; archives are recoverable. It also hard-protects
	// the curator-protected builtins from autonomous removal.
	ArchiveOnDelete bool
	WriteApproval   bool
	BypassGate      bool
	Origin          string
}

type skillManageArgs struct {
	Action      string `json:"action"`
	Name        string `json:"name"`
	Content     string `json:"content"`
	OldString   string `json:"old_string"`
	NewString   string `json:"new_string"`
	ReplaceAll  bool   `json:"replace_all"`
	Category    string `json:"category"`
	FilePath    string `json:"file_path"`
	FileContent string `json:"file_content"`
	AbsorbedInto string `json:"absorbed_into"`
}

func ExecuteSkillManage(argsJSON string, opts SkillManageOptions) (string, bool) {
	var args skillManageArgs
	_ = json.Unmarshal([]byte(argsJSON), &args)

	var result SkillManageResult
	var err error

	if opts.WriteApproval && !opts.BypassGate {
		payload := map[string]interface{}{
			"action":        args.Action,
			"name":          args.Name,
			"content":       args.Content,
			"old_string":    args.OldString,
			"new_string":    args.NewString,
			"replace_all":   args.ReplaceAll,
			"category":      args.Category,
			"file_path":     args.FilePath,
			"file_content":  args.FileContent,
			"absorbed_into": args.AbsorbedInto,
		}

		gist := approval.SkillGist(args.Action, args.Name, args.Content, args.FilePath, args.OldString, args.NewString)
		origin := opts.Origin
		if origin == "" {
			origin = "agent"
		}

		record, err := approval.StageWrite(approval.SubsystemSkills, payload, gist, origin)
		if err != nil {
			result = SkillManageResult{
				Success: false,
				Error:   "Staging failed: " + err.Error(),
			}
			outBytes, _ := json.MarshalIndent(result, "", "  ")
			return string(outBytes), true
		}

		result = SkillManageResult{
			Success:   true,
			Staged:    true,
			PendingID: record.ID,
			Gist:      gist,
			Message:   fmt.Sprintf("Staged for approval (skills.write_approval is on). Not yet saved — review with /skills pending."),
		}
		outBytes, _ := json.MarshalIndent(result, "", "  ")
		return string(outBytes), false
	}

	switch args.Action {
	case "create":
		result, err = createSkill(args.Name, args.Content, args.Category, opts.GuardEnabled)
	case "edit":
		result, err = editSkill(args.Name, args.Content, opts.GuardEnabled)
	case "patch":
		result, err = patchSkill(args.Name, args.OldString, args.NewString, args.FilePath, args.ReplaceAll, opts.GuardEnabled)
	case "delete":
		if opts.ArchiveOnDelete {
			result, err = archiveDeleteSkill(args.Name, args.AbsorbedInto)
		} else {
			result, err = deleteSkill(args.Name, args.AbsorbedInto, opts.GuardEnabled)
		}
	case "write_file":
		result, err = writeSkillFile(args.Name, args.FilePath, args.FileContent, opts.GuardEnabled)
	case "remove_file":
		result, err = removeSkillFile(args.Name, args.FilePath)
	default:
		result = SkillManageResult{
			Success: false,
			Error:   "Unknown action '" + args.Action + "'. Use: create, edit, patch, delete, write_file, remove_file",
		}
	}

	if err != nil {
		result = SkillManageResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	if result.Success {
		switch args.Action {
		case "create":
			if opts.MarkCreatedAsAgent {
				MarkAgentCreated(args.Name)
			}
		case "patch", "edit", "write_file", "remove_file":
			BumpPatch(args.Name)
		case "delete":
			// Archived skills keep their usage record (state=archived was
			// set by ArchiveSkill); hard deletes forget it.
			if !opts.ArchiveOnDelete {
				Forget(args.Name)
			}
		}
	}

	outBytes, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		return `{"success": false, "error": "json marshal error"}`, true
	}
	return string(outBytes), !result.Success
}

func ApplySkillPending(payload map[string]interface{}, opts SkillManageOptions) (SkillManageResult, error) {
	argsBytes, err := json.Marshal(payload)
	if err != nil {
		return SkillManageResult{Success: false, Error: err.Error()}, err
	}
	opts.BypassGate = true
	resJSON, isErr := ExecuteSkillManage(string(argsBytes), opts)
	var res SkillManageResult
	if err := json.Unmarshal([]byte(resJSON), &res); err != nil {
		return SkillManageResult{Success: false, Error: err.Error()}, err
	}
	if isErr && res.Error == "" {
		res.Error = "Action failed"
	}
	return res, nil
}

