package board

// 分组：Excalidraw 用 groupIds 数组表示（数组是为了嵌套），这里只做一层，
// 但结构保持兼容——读进来的嵌套分组也认得，点任何一个成员会选中整组。

// Group 把一批元素编成一组，返回新的组 ID。
func (s *Scene) Group(ids []string) string {
	if len(ids) < 2 {
		return ""
	}
	gid := newID()
	for _, id := range ids {
		if el := s.Find(id); el != nil {
			el.GroupIDs = append(el.GroupIDs, gid)
		}
	}
	return gid
}

// Ungroup 把一批元素身上的分组标记全部去掉。
func (s *Scene) Ungroup(ids []string) {
	for _, id := range ids {
		if el := s.Find(id); el != nil {
			el.GroupIDs = nil
		}
	}
}

// ExpandGroup 返回与 id 同组的全部元素 ID（没分组就返回自己）。
func (s *Scene) ExpandGroup(id string) []string {
	el := s.Find(id)
	if el == nil {
		return nil
	}
	if len(el.GroupIDs) == 0 {
		return []string{id}
	}
	out := []string{id}
	seen := map[string]bool{id: true}
	for _, gid := range el.GroupIDs {
		for i := range s.Elements {
			other := &s.Elements[i]
			if seen[other.ID] || other.Deleted {
				continue
			}
			for _, g := range other.GroupIDs {
				if g == gid {
					seen[other.ID] = true
					out = append(out, other.ID)
					break
				}
			}
		}
	}
	return out
}
