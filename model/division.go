package model

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Administrative division codes (GB/T 2260) mapped to province/city/district names.
// Data: https://github.com/modood/Administrative-divisions-of-China (MIT),
// sourced from the Ministry of Civil Affairs / National Bureau of Statistics.
//
//go:embed pca-code.json
var divisionCodeJSON []byte

type divisionNode struct {
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Children []divisionNode `json:"children"`
}

type divisionInfo struct {
	province string
	city     string
	district string
}

var divisionTable = buildDivisionTable()

func buildDivisionTable() map[string]divisionInfo {
	var roots []divisionNode
	if err := json.Unmarshal(divisionCodeJSON, &roots); err != nil {
		panic("model: invalid embedded pca-code.json: " + err.Error())
	}

	municipalities := map[string]bool{"北京市": true, "天津市": true, "上海市": true, "重庆市": true}

	table := make(map[string]divisionInfo)
	var walk func(nodes []divisionNode, province, city string)
	walk = func(nodes []divisionNode, province, city string) {
		for _, node := range nodes {
			info := divisionInfo{province: province}
			switch len(node.Code) {
			case 2: // province level
				info.province = node.Name
				if municipalities[node.Name] {
					info.city = node.Name
				}
				table[node.Code] = info
				walk(node.Children, node.Name, node.Name)
			case 4: // prefecture level
				info.city = displayCity(node.Name, province)
				table[node.Code] = info
				walk(node.Children, province, node.Name)
			case 6: // county level
				if strings.Contains(city, "直辖县") {
					// county-level city administered directly by the province
					// (e.g. 济源市): shown as the city itself
					info.city, info.district = node.Name, ""
				} else {
					info.city = displayCity(city, province)
					info.district = node.Name
				}
				table[node.Code] = info
			}
		}
	}
	walk(roots, "", "")
	return table
}

// displayCity normalizes prefecture names: the generic 市辖区/县 rows of
// municipalities take the province name, aggregated 直辖县级 rows collapse
// to just the province.
func displayCity(city, province string) string {
	switch city {
	case "市辖区", "县", "":
		return province
	}
	if strings.Contains(city, "直辖县") {
		return ""
	}
	return city
}

// ResolveDivision maps a GeoCN division_code to province/city/district names.
// Codes come in three granularities: XX0000 (province), XXXX00 (prefecture)
// and full six digits (county). Unresolvable codes fall back to the province
// level; if even that fails, all names stay empty.
func ResolveDivision(code uint) (province, city, district string) {
	if code == 0 || code > 999999 {
		return
	}
	s := fmt.Sprintf("%06d", code)
	if info, ok := divisionTable[s]; ok {
		return info.province, info.city, info.district
	}
	if info, ok := divisionTable[s[:4]]; ok {
		return info.province, info.city, ""
	}
	if info, ok := divisionTable[s[:2]]; ok {
		return info.province, info.city, ""
	}
	return
}
