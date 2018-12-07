package scribe

import (
	"fmt"
	"strings"
	"time"

	"github.com/payvision-development/scribe/freshservice"
	"github.com/payvision-development/scribe/vss"
)

type state struct {
	ChangeID           int64
	LastEvent          *vss.Event
	RequestItilChange  *freshservice.RequestItilChange
	ResponseItilChange *freshservice.ResponseItilChange
}

// Session func
func Session(tc uint32, ch chan *vss.Event, fs *freshservice.Freshservice, v *vss.TFS) {

	s := state{}

	for {
		select {
		case event := <-ch:
			switch et := event.EventType; et {

			case vss.DeploymentStartedEvent:

				fmt.Printf("[Release: %v] Event: %v\n", tc, vss.DeploymentStartedEvent)

				desc, err := composeDescription(event, v)
				if err != nil {
					fmt.Println(err)
				}

				err = createChange(event.ReleaseName, event.EnvironmentName, desc, event.Timestamp, &s, fs)
				if err != nil {
					fmt.Println(err)
				} else {
					err := updateChange(event.DetailedMessageHTML, freshservice.StatusOpen, &s, fs)
					if err != nil {
						fmt.Println(err)
					} else {
						if nil != s.LastEvent && vss.DeploymentApprovalPendingEvent == s.LastEvent.EventType {
							var status int

							if "preDeploy" == s.LastEvent.ApprovalType {
								status = freshservice.StatusAwaitingApproval
							} else {
								status = freshservice.StatusPendingReview
							}

							err := updateChange(s.LastEvent.DetailedMessageHTML, status, &s, fs)
							if err != nil {
								fmt.Println(err)
							}
						}
					}
				}

			case vss.DeploymentApprovalPendingEvent:

				fmt.Printf("[Release: %v] Event: %v\n", tc, vss.DeploymentApprovalPendingEvent)

				if 0 != s.ChangeID {
					var status int

					if "preDeploy" == event.ApprovalType {
						status = freshservice.StatusAwaitingApproval
					} else {
						status = freshservice.StatusPendingReview
					}

					err := updateChange(event.DetailedMessageHTML, status, &s, fs)
					if err != nil {
						fmt.Println(err)
					}
				}

			case vss.DeploymentApprovalCompletedEvent:

				fmt.Printf("[Release: %v] Event: %v\n", tc, vss.DeploymentApprovalCompletedEvent)

				if 0 != s.ChangeID {
					var status int

					if "preDeploy" == event.ApprovalType {
						status = freshservice.StatusPendingRelease
					} else {
						status = freshservice.StatusOpen
					}

					err := updateChange(event.DetailedMessageHTML, status, &s, fs)
					if err != nil {
						fmt.Println(err)
					}
				}

			case vss.DeploymentCompletedEvent:

				fmt.Printf("[Release: %v] Event: %v\n", tc, vss.DeploymentCompletedEvent)

				if 0 != s.ChangeID {
					err := updateChange(event.DetailedMessageHTML, freshservice.StatusClosed, &s, fs)
					if err != nil {
						fmt.Println(err)
					}
				}
			}

			s.LastEvent = event

		case <-time.After(30 * time.Minute):

			if 0 != s.ChangeID && s.LastEvent.EventType != vss.DeploymentCompletedEvent {

				fmt.Printf("[Release: %v] Event: %v\n", tc, "Deployment timeout")

				err := updateChange("Deployment timeout<br>Status: Failed", freshservice.StatusClosed, &s, fs)
				if err != nil {
					fmt.Println(err)
				}
			}

			return
		}
	}
}

func composeDescription(event *vss.Event, v *vss.TFS) (string, error) {
	var s strings.Builder

	if v != nil {
		r, err := v.GetRelease(event.ProjectID, event.ReleaseID)
		if err != nil {
			return "", err
		}

		ws, err := v.GetWorkItems(event.ProjectID, r.Artifacts[0].DefinitionReference.Version.ID)
		if err != nil {
			return "", err
		}

		if ws.Count != 0 {
			s.WriteString("<br><b>Work Items to deploy</b><br>")

			for _, item := range ws.Value {
				w, err := v.GetWorkItem(item.ID)
				if err != nil {
					return "", err
				}

				s.WriteString("<br>[" + w.Fields.SystemWorkItemType + "] <a href='" + w.Links.HTML.Href + "'>" + w.Fields.SystemTitle + "</a>")
			}

			return s.String(), nil
		}
	}

	s.WriteString("<br><b>No Work Items associated to this deploy</b><br>")
	return s.String(), nil
}

func createChange(name string, environment string, msg string, date string, s *state, fs *freshservice.Freshservice) error {

	c := &freshservice.RequestItilChange{}

	c.ItilChange.Subject = "[Release Management] Deployment of release " + name + " to environment " + environment
	c.ItilChange.DescriptionHTML = msg
	c.ItilChange.Status = freshservice.StatusPendingRelease
	c.ItilChange.Priority = freshservice.PriorityMedium
	c.ItilChange.ChangeType = freshservice.TypeStandard
	c.ItilChange.Risk = freshservice.RiskMedium
	c.ItilChange.Impact = freshservice.ImpactMedium
	c.ItilChange.PlannedStartDate = date
	c.ItilChange.PlannedEndDate = date

	s.RequestItilChange = c

	resItilChange, err := fs.CreateChange(c)
	if err != nil {
		return err
	}

	s.ResponseItilChange = resItilChange

	return nil
}

func updateChange(msg string, status int, s *state, fs *freshservice.Freshservice) error {

	n := &freshservice.RequestNote{}
	n.Note.BodyHTML = msg

	_, err := fs.AddChangeNote(s.ResponseItilChange.Item.ItilChange.DisplayID, n)
	if err != nil {
		return err
	}

	s.RequestItilChange.ItilChange.Status = status
	s.RequestItilChange.ItilChange.PlannedStartDate = ""
	s.RequestItilChange.ItilChange.PlannedEndDate = ""

	_, err = fs.UpdateChange(s.ResponseItilChange.Item.ItilChange.DisplayID, s.RequestItilChange)
	if err != nil {
		return err
	}

	return nil
}
