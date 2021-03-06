package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/ghodss/yaml"
	"github.com/grrtrr/clccam"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/spf13/cobra"
)

var (
	cmdBoxes = &cobra.Command{ // Top-level command
		Use:   "box",
		Short: "Manage boxes",
	}

	// boxList lists one or more boxes
	boxList = &cobra.Command{
		Use:     "ls  [boxId, ...]",
		Aliases: []string{"list", "show", "get"},
		Short:   "List box(es)",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				if boxes, err := client.GetBoxes(); err != nil {
					die("failed to query box list: %s", err)
				} else if cmd.Flags().Lookup("json").Value.String() != "true" {
					listBoxes(boxes)
				}
			} else {
				for _, boxId := range args {
					if box, err := client.GetBox(boxId); err != nil {
						fmt.Fprintf(os.Stderr, "Failed to query box %s: %s\n", boxId, err)
					} else if cmd.Flags().Lookup("json").Value.String() != "true" {
						listBoxes([]clccam.Box{box})
					}
				}
			}
		},
	}

	// boxImport imports a Box Directory containing box files as a new box.
	boxImportFlags struct {
		AsDraft bool   // Whether to bypass the normal import process
		Raw     bool   // Whether to use 'raw' import mode
		Owner   string // Override the box owner
	}
	boxImport = &cobra.Command{
		Use:     "import </path/to/box/directory>",
		Aliases: []string{"imp", "up", "upload"},
		Short:   "Import box from directory",
		PreRunE: checkArgs(1, "Need a box directory"),
		Run: func(cmd *cobra.Command, args []string) {
			var fileVariables []clccam.BasicVariable

			res, err := importBox(args[0], boxImportFlags.Owner, boxImportFlags.AsDraft, boxImportFlags.Raw)
			if err != nil {
				die("%s", err)
			} else if cmd.Flags().Lookup("json").Value.String() == "true" {
				return
			}
			if res.URI != nil {
				fmt.Printf("Updloaded box %s to %v\n", res.ID, res.URI)
			} else {
				fmt.Printf("Updloaded box %s\n", res.ID)
			}

			for _, v := range res.Variables {
				if v.Type == "File" {
					fileVariables = append(fileVariables, v.BasicVariable)
				}
			}
			if len(fileVariables) > 0 {
				fmt.Println("Variables:")
				for _, v := range fileVariables {
					fmt.Printf("%60s  %s\n", v.Name, v.Value)
				}
			}
		},
	}

	// boxStack prints the box stack
	boxStack = &cobra.Command{
		Use:     "stack  boxId",
		Short:   "List the stack of @boxId (if any)",
		PreRunE: checkArgs(1, "Need a box ID"),
		Run: func(cmd *cobra.Command, args []string) {
			if boxes, err := client.GetBoxStack(args[0]); err != nil {
				die("failed to query %s box stack: %s", args[0], err)
			} else if cmd.Flags().Lookup("json").Value.String() != "true" {
				var filtered []clccam.Box

				// The output is unsorted. Move box in question to the top of the list.
				for i, box := range boxes {
					if box.ID.String() == args[0] && i > 0 {
						filtered = append([]clccam.Box{box}, filtered...)
					} else {
						filtered = append(filtered, box)
					}
				}
				listBoxes(filtered)
			}
		},
	}

	// boxVersions prints box versions
	boxVersions = &cobra.Command{
		Use:     "ver  boxId",
		Aliases: []string{"version"},
		Short:   "List version(s) of @boxId",
		PreRunE: checkArgs(1, "Need a box ID"),
		Run: func(cmd *cobra.Command, args []string) {
			if boxes, err := client.GetBoxVersions(args[0]); err != nil {
				die("failed to query %s box versions: %s", args[0], err)
			} else if cmd.Flags().Lookup("json").Value.String() == "true" {
			} else if len(boxes) == 0 {
				fmt.Printf("No versions available.\n")
			} else {
				var table = tablewriter.NewWriter(os.Stdout)

				table.SetAutoFormatHeaders(false)
				table.SetAlignment(tablewriter.ALIGN_LEFT)
				table.SetAutoWrapText(true)

				table.SetHeader([]string{
					"Name", "Version", "ID", "Owner", "Visibility", "Created", "Updated",
				})
				sort.Slice(boxes, func(i, j int) bool {
					return boxes[i].Version().LessThan(boxes[j].Version())
				})
				for _, b := range boxes {
					table.Append([]string{
						b.Name,
						b.Version().String(),
						b.ID.String(),
						b.Owner,
						b.Visibility.String(),
						humanize.Time(b.Created.Local()), humanize.Time(b.Updated.Local()),
					})
				}
				table.Render()
			}
		},
	}

	// boxDiff prints differences between boxes
	boxDiff = &cobra.Command{
		Use:     "diff  boxId",
		Short:   "Print the differences of @boxId",
		PreRunE: checkArgs(1, "Need a box ID"),
		Run: func(cmd *cobra.Command, args []string) {
			// NOTE/FIXME: seems to be privileged or not used, getting 405 response; no other documentation.
			if err := client.WithJsonResponse().GetBoxDiff(args[0]); err != nil {
				die("failed to query %s box differences: %s", args[0], err)
			}
		},
	}

	// boxBindings prints bindings
	boxBindings = &cobra.Command{
		Use:     "bindings  boxId",
		Aliases: []string{"bdg"},
		Short:   "List the bindings of @boxId",
		PreRunE: checkArgs(1, "Need a box ID"),
		Run: func(cmd *cobra.Command, args []string) {
			if bindings, err := client.GetBoxBindings(args[0]); err != nil {
				die("failed to query %s box bindings: %s", args[0], err)
			} else if cmd.Flags().Lookup("json").Value.String() != "true" {
				var table = tablewriter.NewWriter(os.Stdout)

				table.SetAutoFormatHeaders(false)
				table.SetAlignment(tablewriter.ALIGN_LEFT)
				table.SetAutoWrapText(true)
				table.SetHeader([]string{"Name", "ID", "Icon"})

				for _, b := range bindings {
					table.Append([]string{b.Name, b.ID.String(), b.Icon.String()})
				}
				table.Render()
			}
		},
	}

	// boxDelete removes a box
	boxDelete = &cobra.Command{
		Use:     "rm  boxId [boxId1...]",
		Aliases: []string{"delete", "del", "remove"},
		Short:   "Remove box",
		PreRunE: checkAtLeastArgs(1, "Need at least one box ID"),
		Run: func(cmd *cobra.Command, args []string) {
			for _, arg := range args {
				if err := client.DeleteBox(arg); err != nil {
					fmt.Printf("FATAL: failed to delete box %s: %s\n", arg, err)
				} else {
					fmt.Printf("Deleted box %s.\n", arg)
				}
			}
		},
	}
)

// listBoxes prints a table with a subset of box information for each of the @boxes.
func listBoxes(boxes []clccam.Box) {
	if len(boxes) == 0 {
		fmt.Println("No boxes.")
	} else {
		var table = tablewriter.NewWriter(os.Stdout)
		const timefmt = `Mon Jan  _2 15:04 MST 2006`

		table.SetAutoFormatHeaders(false)
		table.SetAlignment(tablewriter.ALIGN_LEFT)
		table.SetAutoWrapText(true)

		table.SetHeader([]string{
			"Name", "ID", "Owner", "Visibility", "Created", "Updated",
		})
		for _, b := range boxes {
			var created = "Not set"
			var updated = "Not set"

			if !b.Created.IsZero() {
				created = humanize.Time(b.Created.Local())
			}
			if !b.Updated.IsZero() {
				updated = humanize.Time(b.Updated.Local())
			}

			table.Append([]string{
				b.Name,
				b.ID.String(),
				b.Owner,
				b.Visibility.String(),
				created,
				updated,
			})
		}
		table.Render()
	}
}

func init() {
	boxImport.Flags().BoolVar(&boxImportFlags.AsDraft, "as-draft", true, "Upload box as draft (non-raw mode only)")
	boxImport.Flags().BoolVar(&boxImportFlags.Raw, "raw", false, "Use raw import mode")
	boxImport.Flags().StringVarP(&boxImportFlags.Owner, "owner", "o", "", "If set, overrides the box owner")

	cmdBoxes.AddCommand(boxList, boxStack, boxVersions, boxDiff, boxBindings, boxImport, boxDelete)
	Root.AddCommand(cmdBoxes)
}

// importBox processes a box directory @boxDir and tries to import this as a box.
// @owner:     override box owner
// @asDraft:   submit box as draft
// @rawImport: use raw import mode
func importBox(boxDir, owner string, asDraft, rawImport bool) (*clccam.Box, error) {
	var (
		box        clccam.Box
		current    *clccam.Box        // Existing variant of this box
		existingId string             // controls upload: create new or replace
		uploaderFn = client.UploadBox // determines standard or raw (appliance box) import
	)

	if fi, err := os.Stat(boxDir); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		return nil, errors.Errorf("not a directory: %q", boxDir)
	}

	// Sometimes a 'draft' directory is inserted between the directory and its contents.
	if _, err := os.Stat(path.Join(boxDir, "draft", "box.yaml")); err == nil {
		boxDir = path.Join(boxDir, "draft")
	}

	if content, err := ioutil.ReadFile(path.Join(boxDir, "box.yaml")); err != nil {
		return nil, errors.Errorf("unable to read box.yaml: %s", err)
	} else if err = yaml.Unmarshal(content, &box); err != nil {
		return nil, errors.Wrapf(err, "failed to deserialize box.yaml")
	}

	if asDraft && uuid.Equal(uuid.Nil, box.ID) {
		return nil, errors.Errorf("box without ID can not be uploaded as draft")
	}

	if rawImport {
		box.Visibility = clccam.Visibility_Public
		uploaderFn = client.UploadApplianceBox
	}

	// See if it replaces an existing box of the same ID.
	if !uuid.Equal(uuid.Nil, box.ID) {
		if existing, err := client.GetBox(box.ID.String()); err != nil {
			/* Ignore error here. Call is only used to see if Box already exists. */
		} else {
			current = &existing
		}
	}

	if current == nil {
		if box.Organization == "" {
			box.Organization = "elasticbox"
		}
	} else { // Updating existing box
		existingId = box.ID.String()

		// Copy pre-existing configuration.
		owner = current.Owner

		// Do not copy members, sometimes they contain invalid entries.
		// box.Members = current.Members

		if box.Organization == "" {
			box.Organization = current.Organization
		}

		if rawImport && len(box.Categories) == 0 {
			box.Categories = current.Categories
		}

		if !asDraft || box.BoxVersion != nil {
			if current.BoxVersion == nil {
				box.BoxVersion = &clccam.BoxVersion{
					Box: box.ID,
				}
			} else {
				box.BoxVersion = current.BoxVersion
			}
			box.BoxVersion.Description = "Version created using a kind of ebcli"
		}
	}

	// Always make sure that there is an empty list, not a nil value
	if len(box.Members) == 0 {
		box.Members = []clccam.WorkSpaceMember{}
	}

	// Override owner if present, fall back to owner of authorization token if no owner present.
	if owner != "" {
		box.Owner = owner
	} else if box.Owner == "" {
		user, err := client.GetTokenSubject()
		if err != nil {
			return nil, errors.Wrapf(err, "unable to determine token owner")
		}
		box.Owner = user
	}

	// Schema: default to Script Box if not set.
	if box.Schema.IsZero() {
		s, err := clccam.UriFromString(clccam.ScriptBoxSchema)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to set Script Box schema")
		}
		box.Schema = *s
	}

	// For Script Box types, collect the event scripts.
	if box.Schema.String() == clccam.ScriptBoxSchema {

		box.Events = make(map[clccam.BoxEvent]clccam.Event)

		for _, evtName := range clccam.BoxEventStrings() {
			var evtPath = path.Join(boxDir, "events", evtName)
			var evt clccam.BoxEvent

			if _, err := os.Stat(evtPath); err == nil {
				if err := evt.Set(evtName); err != nil {
					return nil, errors.Wrapf(err, "unable to set %s event", evtName)
				}
				if b, err := ioutil.ReadFile(evtPath); err != nil {
					return nil, errors.Errorf("unable to read %q event file: %s", evtName, err)
				} else if res, err := client.UploadFile(evtName, b); err != nil {
					return nil, errors.Errorf("failed to upload %q event file: %s", evtName, err)
				} else {
					box.Events[evt] = clccam.Event{BlobResponse: res}
				}
			}
		}
	}

	// Variables loaded from file, identified by a 'File' type.
	for i, v := range box.Variables {
		if v.Type == "File" && v.Value != "" {
			if b, err := ioutil.ReadFile(path.Join(boxDir, v.Value)); err != nil {
				return nil, errors.Errorf("unable to read File variable %s at %s: %s", v.Name, v.Value, err)
			} else if res, err := client.UploadFile(path.Base(v.Value), b); err != nil {
				return nil, errors.Errorf("failed to upload File variable %s: %s", v.Name, err)
			} else {
				box.Variables[i].Value = res.Url.String()
			}
		}
	}

	// Icon files
	if box.IconMetadata != nil && !box.IconMetadata.Image.IsZero() {
		if p := box.IconMetadata.Image.Path; !strings.HasPrefix(p, "images/") {
			// FIXME: if uploading a new image under a different name from the one
			//        already recorded in the DB, the file name is not updated below.
			//        This is slightly different from the ebcli code.
			if b, err := ioutil.ReadFile(path.Join(boxDir, p)); err != nil {
				return nil, errors.Errorf("unable to read icon file %q: %s", p, err)
			} else if res, err := client.UploadFile(path.Base(p), b); err != nil {
				return nil, errors.Errorf("failed to upload icon file %q: %s", p, err)
			} else {
				box.IconMetadata.Image = res.Url
			}
		}
	} else if box.Icon != "" {
		if b, err := ioutil.ReadFile(path.Join(boxDir, box.Icon)); err != nil {
			return nil, errors.Errorf("unable to read icon file %q: %s", box.Icon, err)
		} else if res, err := client.UploadFile(path.Base(box.Icon), b); err != nil {
			return nil, errors.Errorf("failed to upload icon file %q: %s", box.Icon, err)
		} else {
			box.Icon = res.Url.String()
		}
	}

	// readme.MD file
	if _, err := os.Stat(path.Join(boxDir, clccam.ReadmeName)); err == nil {
		if b, err := ioutil.ReadFile(path.Join(boxDir, clccam.ReadmeName)); err != nil {
			return nil, errors.Errorf("unable to read 'read-me' file: %s", err)
		} else if box.Readme, err = client.UploadFile(clccam.ReadmeName, b); err != nil {
			return nil, errors.Errorf("failed to upload readme file: %s", err)
		}
	}

	// Upload
	if box.BoxVersion != nil {
		fmt.Println("WARNING: box version handling is not fully functional yet")
		if !uuid.Equal(box.BoxVersion.Box, box.ID) {
			return uploaderFn(&box, box.BoxVersion.Box.String())
		}
	} else {
		box.Created = clccam.Timestamp{Time: time.Now().UTC()}
	}

	return uploaderFn(&box, existingId)
}
