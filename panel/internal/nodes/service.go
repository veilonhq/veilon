package nodes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/xray"
)

// InstallScript is the one-line installer the node command uses.
const InstallScript = "https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh"

// ValidationError is a bad request.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// ConflictError is a duplicate name.
type ConflictError struct{ Msg string }

func (e *ConflictError) Error() string { return e.Msg }

// ErrNotFound is returned for an unknown node id.
var ErrNotFound = errors.New("node not found")

// Service manages nodes for the API.
type Service struct {
	DB      *gorm.DB
	PKI     *PKI
	Manager *Manager
}

// View is a node as the API shows it.
type View struct {
	db.Node
	InboundIDs []int64 `json:"inbound_ids"`
	State      State   `json:"state"`
}

// Install is the one-time setup for a node.
type Install struct {
	Token   string `json:"token"`
	Command string `json:"command"`
	Port    int    `json:"port"`
}

// Input creates or changes a node; nil fields are left as they are.
type Input struct {
	Name       *string  `json:"name"`
	Address    *string  `json:"address"`
	APIPort    *int     `json:"api_port"`
	Enabled    *bool    `json:"enabled"`
	InboundIDs *[]int64 `json:"inbound_ids"`
}

var hostRe = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

func validHost(h string) bool {
	if net.ParseIP(h) != nil {
		return true
	}
	return len(h) <= 253 && hostRe.MatchString(h) && strings.Contains(h, ".")
}

// List returns every node with its live state.
func (s *Service) List() ([]View, error) {
	var ns []db.Node
	if err := s.DB.Order("id").Find(&ns).Error; err != nil {
		return nil, err
	}
	ids := InboundIDs(s.DB)
	out := make([]View, 0, len(ns))
	for i := range ns {
		v := View{Node: ns[i], InboundIDs: ids[ns[i].ID], State: s.Manager.Status(&ns[i])}
		if v.InboundIDs == nil {
			v.InboundIDs = []int64{}
		}
		out = append(out, v)
	}
	return out, nil
}

// Get returns one node.
func (s *Service) Get(id int64) (*View, error) {
	var n db.Node
	if err := s.DB.First(&n, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var ids []int64
	s.DB.Model(&db.NodeInbound{}).Where("node_id = ?", id).Order("inbound_id").Pluck("inbound_id", &ids)
	if ids == nil {
		ids = []int64{}
	}
	return &View{Node: n, InboundIDs: ids, State: s.Manager.Status(&n)}, nil
}

func (s *Service) check(n *db.Node, ids *[]int64) error {
	n.Name = strings.TrimSpace(n.Name)
	n.Address = strings.TrimSpace(strings.Trim(n.Address, "[]"))
	if n.Name == "" || utf8.RuneCountInString(n.Name) > 48 {
		return &ValidationError{"name must be 1-48 characters"}
	}
	if !validHost(n.Address) {
		return &ValidationError{"address must be an IP address or a domain name (no scheme, port or path)"}
	}
	if n.APIPort < 1 || n.APIPort > 65535 {
		return &ValidationError{"api_port must be 1-65535"}
	}
	var dup int64
	s.DB.Model(&db.Node{}).Where("LOWER(name) = LOWER(?) AND id <> ?", n.Name, n.ID).Count(&dup)
	if dup > 0 {
		return &ConflictError{fmt.Sprintf("a node named %q already exists", n.Name)}
	}
	if ids != nil {
		seen := map[int64]bool{}
		clean := []int64{}
		for _, id := range *ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			var ib db.Inbound
			if err := s.DB.First(&ib, id).Error; err != nil {
				return &ValidationError{fmt.Sprintf("inbound %d does not exist", id)}
			}
			if !xray.NodeCapable(ib.Protocol) {
				return &ValidationError{fmt.Sprintf("%s (%s) cannot run on a node; nodes serve Xray inbounds only", ib.Remark, ib.Protocol)}
			}
			clean = append(clean, id)
		}
		*ids = clean
	}
	return nil
}

func setInbounds(tx *gorm.DB, nodeID int64, ids []int64) error {
	if err := tx.Where("node_id = ?", nodeID).Delete(&db.NodeInbound{}).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := tx.Create(&db.NodeInbound{NodeID: nodeID, InboundID: id}).Error; err != nil {
			return err
		}
	}
	return nil
}

// Create adds a node and returns its install command.
func (s *Service) Create(in Input) (*View, *Install, error) {
	n := db.Node{APIPort: DefaultPort, Enabled: true}
	if in.Name != nil {
		n.Name = *in.Name
	}
	if in.Address != nil {
		n.Address = *in.Address
	}
	if in.APIPort != nil {
		n.APIPort = *in.APIPort
	}
	if in.Enabled != nil {
		n.Enabled = *in.Enabled
	}
	ids := []int64{}
	if in.InboundIDs != nil {
		ids = *in.InboundIDs
	}
	if err := s.check(&n, &ids); err != nil {
		return nil, nil, err
	}
	var inst *Install
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&n).Error; err != nil {
			return err
		}
		e, pin, err := s.PKI.Enroll(n.ID, n.APIPort)
		if err != nil {
			return err
		}
		if err := tx.Model(&n).Update("cert_sha256", pin).Error; err != nil {
			return err
		}
		n.CertSHA256 = pin
		inst = install(e)
		return setInbounds(tx, n.ID, ids)
	})
	if err != nil {
		return nil, nil, err
	}
	s.Manager.Kick()
	v, err := s.Get(n.ID)
	return v, inst, err
}

// Update changes a node.
func (s *Service) Update(id int64, in Input) (*View, error) {
	var n db.Node
	if err := s.DB.First(&n, id).Error; err != nil {
		return nil, ErrNotFound
	}
	oldPort := n.APIPort
	if in.Name != nil {
		n.Name = *in.Name
	}
	if in.Address != nil {
		n.Address = *in.Address
	}
	if in.APIPort != nil {
		n.APIPort = *in.APIPort
	}
	if in.Enabled != nil {
		n.Enabled = *in.Enabled
	}
	if err := s.check(&n, in.InboundIDs); err != nil {
		return nil, err
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.Node{}).Where("id = ?", id).Updates(map[string]any{
			"name": n.Name, "address": n.Address, "api_port": n.APIPort, "enabled": n.Enabled, "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		if in.InboundIDs != nil {
			return setInbounds(tx, id, *in.InboundIDs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if oldPort != n.APIPort {
		s.Manager.Forget(id)
	}
	s.Manager.Kick()
	return s.Get(id)
}

// Enroll issues a new certificate (the old one stops working) and returns
// a fresh install command, e.g. for reinstalling a node.
func (s *Service) Enroll(id int64) (*Install, error) {
	var n db.Node
	if err := s.DB.First(&n, id).Error; err != nil {
		return nil, ErrNotFound
	}
	e, pin, err := s.PKI.Enroll(n.ID, n.APIPort)
	if err != nil {
		return nil, err
	}
	if err := s.DB.Model(&db.Node{}).Where("id = ?", id).Update("cert_sha256", pin).Error; err != nil {
		return nil, err
	}
	s.Manager.Forget(id)
	s.Manager.Kick()
	return install(e), nil
}

// Delete removes a node. The node is first told (best effort, a few
// seconds at most) to stop serving, so its users lose access with it.
func (s *Service) Delete(id int64) error {
	var n db.Node
	if err := s.DB.First(&n, id).Error; err != nil {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	_ = s.Manager.Reset(ctx, &n)
	cancel()
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var ids []int64
		tx.Model(&db.NodeInbound{}).Where("node_id = ?", id).Pluck("inbound_id", &ids)
		if err := tx.Where("node_id = ?", id).Delete(&db.NodeInbound{}).Error; err != nil {
			return err
		}
		// An inbound that ran on this node only has no node left: it goes
		// back to the main server (the flag is set so the stored state says
		// what happens).
		if len(ids) > 0 {
			if err := tx.Model(&db.Inbound{}).Where("id IN ? AND id NOT IN (?)", ids,
				tx.Table("node_inbounds").Select("inbound_id")).Update("serve_main", true).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&db.Node{}, id).Error
	})
	if err == nil {
		s.Manager.Forget(id)
	}
	return err
}

func install(e *Enrollment) *Install {
	tok := e.Token()
	return &Install{Token: tok, Port: e.Port,
		Command: fmt.Sprintf("bash <(curl -Ls %s) node --token %s", InstallScript, tok)}
}
