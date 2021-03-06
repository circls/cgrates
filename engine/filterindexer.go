/*
Real-time Online/Offline Charging System (OCS) for Telecom & ISP environments
Copyright (C) ITsysCOM GmbH

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package engine

import (
	"fmt"
	"strings"

	"github.com/cgrates/cgrates/cache"
	"github.com/cgrates/cgrates/utils"
)

func NewReqFilterIndexer(dm *DataManager, itemType, dbKeySuffix string) *ReqFilterIndexer {
	return &ReqFilterIndexer{dm: dm, itemType: itemType, dbKeySuffix: dbKeySuffix,
		indexes:          make(map[string]utils.StringMap),
		reveseIndex:      make(map[string]utils.StringMap),
		chngdIndxKeys:    make(utils.StringMap),
		chngdRevIndxKeys: make(utils.StringMap)}
}

// ReqFilterIndexer is a centralized indexer for all data sources using RequestFilter
// retrieves and stores it's data from/to dataDB
// not thread safe, meant to be used as logic within other code blocks
type ReqFilterIndexer struct {
	indexes          map[string]utils.StringMap // map[fieldName:fieldValue]utils.StringMap[itemID]
	reveseIndex      map[string]utils.StringMap // map[itemID]utils.StringMap[fieldName:fieldValue]
	dm               *DataManager
	itemType         string
	dbKeySuffix      string          // get/store the result from/into this key
	chngdIndxKeys    utils.StringMap // keep record of the changed fieldName:fieldValue pair so we can re-cache wisely
	chngdRevIndxKeys utils.StringMap // keep record of the changed itemID so we can re-cache wisely
}

// ChangedKeys returns the changed keys from original indexes so we can reload wisely
func (rfi *ReqFilterIndexer) ChangedKeys(reverse bool) utils.StringMap {
	if reverse {
		return rfi.chngdRevIndxKeys
	}
	return rfi.chngdIndxKeys
}

// IndexTPFilter parses reqFltrs, adding itemID in the indexes and marks the changed keys in chngdIndxKeys
func (rfi *ReqFilterIndexer) IndexTPFilter(tpFltr *utils.TPFilterProfile, itemID string) {
	var hasMetaString bool
	if _, hasIt := rfi.reveseIndex[itemID]; !hasIt {
		rfi.reveseIndex[itemID] = make(utils.StringMap)
	}
	for _, fltr := range tpFltr.Filters {
		if fltr.Type != MetaString {
			continue
		}
		hasMetaString = true // Mark that we found at least one metatring so we don't index globally
		for _, fldVal := range fltr.Values {
			concatKey := utils.ConcatenatedKey(fltr.FieldName, fldVal)
			if _, hasIt := rfi.indexes[concatKey]; !hasIt {
				rfi.indexes[concatKey] = make(utils.StringMap)
			}
			rfi.indexes[concatKey][itemID] = true
			rfi.reveseIndex[itemID][concatKey] = true
			rfi.chngdIndxKeys[concatKey] = true
		}
		rfi.chngdRevIndxKeys[itemID] = true
	}
	if !hasMetaString {
		naConcatKey := utils.ConcatenatedKey(utils.NOT_AVAILABLE, utils.NOT_AVAILABLE)
		if _, hasIt := rfi.indexes[naConcatKey]; !hasIt {
			rfi.indexes[naConcatKey] = make(utils.StringMap)
		}
		rfi.reveseIndex[itemID][naConcatKey] = true
		rfi.indexes[naConcatKey][itemID] = true // Fields without real field index will be located in map[NOT_AVAILABLE:NOT_AVAILABLE][rl.ID]
	}
	return
}

func (rfi *ReqFilterIndexer) cacheRemItemType() {
	switch rfi.itemType {
	case utils.ThresholdProfilePrefix:
		cache.RemPrefixKey(utils.ThresholdStringIndex, true, utils.NonTransactional)
		cache.RemPrefixKey(utils.ThresholdStringRevIndex, true, utils.NonTransactional)

	case utils.ResourceProfilesPrefix:
		cache.RemPrefixKey(utils.ResourceProfilesStringIndex, true, utils.NonTransactional)
		cache.RemPrefixKey(utils.ResourceProfilesStringRevIndex, true, utils.NonTransactional)

	case utils.StatQueueProfilePrefix:
		cache.RemPrefixKey(utils.StatQueuesStringIndex, true, utils.NonTransactional)
		cache.RemPrefixKey(utils.StatQueuesStringRevIndex, true, utils.NonTransactional)

	case utils.SupplierProfilePrefix:
		cache.RemPrefixKey(utils.SupplierProfilesStringIndex, true, utils.NonTransactional)
		cache.RemPrefixKey(utils.SupplierProfilesStringRevIndex, true, utils.NonTransactional)

	case utils.AttributeProfilePrefix:
		cache.RemPrefixKey(utils.AttributeProfilesStringIndex, true, utils.NonTransactional)
		cache.RemPrefixKey(utils.AttributeProfilesStringRevIndex, true, utils.NonTransactional)
	}
}

// StoreIndexes handles storing the indexes to dataDB
func (rfi *ReqFilterIndexer) StoreIndexes() (err error) {
	if err = rfi.dm.SetFilterIndexes(
		GetDBIndexKey(rfi.itemType, rfi.dbKeySuffix, false),
		rfi.indexes); err != nil {
		return
	}
	if err = rfi.dm.SetFilterReverseIndexes(
		GetDBIndexKey(rfi.itemType, rfi.dbKeySuffix, true),
		rfi.reveseIndex); err != nil {
		return
	}
	rfi.cacheRemItemType()
	return
}

//Populate the ReqFilterIndexer.reveseIndex for specifil itemID
func (rfi *ReqFilterIndexer) loadItemReverseIndex(itemID string) (err error) {
	rcvReveseIdx, err := rfi.dm.GetFilterReverseIndexes(
		GetDBIndexKey(rfi.itemType, rfi.dbKeySuffix, true),
		map[string]string{itemID: ""})
	if err != nil {
		return err
	}
	for _, val2 := range rcvReveseIdx {
		if _, has := rfi.reveseIndex[itemID]; !has {
			rfi.reveseIndex[itemID] = make(utils.StringMap)
		}
		rfi.reveseIndex[itemID] = val2
	}
	return err
}

//Populate ReqFilterIndexer.indexes with specific fieldName:fieldValue , item
func (rfi *ReqFilterIndexer) loadFldNameFldValIndex(fldName, fldVal string) error {
	rcvIdx, err := rfi.dm.GetFilterIndexes(
		GetDBIndexKey(rfi.itemType, rfi.dbKeySuffix, false),
		map[string]string{fldName: fldVal})
	if err != nil {
		return err
	}
	for fldName, nameValMp := range rcvIdx {
		if _, has := rfi.indexes[fldName]; !has {
			rfi.indexes[fldName] = make(utils.StringMap)
		}
		rfi.indexes[fldName] = nameValMp
	}
	return nil
}

//RemoveItemFromIndex remove Indexes for a specific itemID
func (rfi *ReqFilterIndexer) RemoveItemFromIndex(itemID string) (err error) {
	if err = rfi.loadItemReverseIndex(itemID); err != nil {
		return err
	}
	for key, _ := range rfi.reveseIndex[itemID] {
		kSplt := strings.Split(key, utils.CONCATENATED_KEY_SEP)
		if len(kSplt) != 2 {
			return fmt.Errorf("Malformed key in db: %s", key)
		}
		if err = rfi.loadFldNameFldValIndex(kSplt[0], kSplt[1]); err != nil {
			return err
		}
	}
	for _, itmMp := range rfi.indexes {
		for range itmMp {
			if _, has := itmMp[itemID]; has {
				delete(itmMp, itemID) //Force deleting in driver
			}
		}
	}
	rfi.reveseIndex[itemID] = make(utils.StringMap) //Force deleting in driver
	if err = rfi.dm.SetFilterIndexes(
		GetDBIndexKey(rfi.itemType, rfi.dbKeySuffix, false),
		rfi.indexes); err != nil {
		return
	}
	if err = rfi.dm.SetFilterReverseIndexes(
		GetDBIndexKey(rfi.itemType, rfi.dbKeySuffix, true),
		rfi.reveseIndex); err != nil {
		return
	}
	return
}

//GetDBIndexKey return the dbKey for an specific item
func GetDBIndexKey(itemType, dbKeySuffix string, reverse bool) (dbKey string) {
	var idxPrefix, rIdxPrefix string
	switch itemType {
	case utils.ThresholdProfilePrefix:
		idxPrefix = utils.ThresholdStringIndex
		rIdxPrefix = utils.ThresholdStringRevIndex
	case utils.ResourceProfilesPrefix:
		idxPrefix = utils.ResourceProfilesStringIndex
		rIdxPrefix = utils.ResourceProfilesStringRevIndex
	case utils.StatQueueProfilePrefix:
		idxPrefix = utils.StatQueuesStringIndex
		rIdxPrefix = utils.StatQueuesStringRevIndex
	case utils.SupplierProfilePrefix:
		idxPrefix = utils.SupplierProfilesStringIndex
		rIdxPrefix = utils.SupplierProfilesStringRevIndex
	case utils.AttributeProfilePrefix:
		idxPrefix = utils.AttributeProfilesStringIndex
		rIdxPrefix = utils.AttributeProfilesStringRevIndex
	}
	if reverse {
		return rIdxPrefix + dbKeySuffix
	}
	return idxPrefix + dbKeySuffix
}
