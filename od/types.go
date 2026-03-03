package od

type ShareType int

const (
	TypeSP ShareType = iota
	TypePersonal
)

func (t ShareType) String() string {
	switch t {
	case TypeSP:
		return "SharePoint/Business"
	case TypePersonal:
		return "OneDrive Personal"
	default:
		return "Unknown"
	}
}

type FileEntry struct {
	Name    string
	Size    int64
	DlURL   string
	RelPath string
}

type ShareInfo struct {
	Type       ShareType
	Files      []FileEntry
	TotalSize  int64
	TotalFiles int
}
