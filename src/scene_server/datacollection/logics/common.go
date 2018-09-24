/*
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2018 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package logics

import (
	"context"
	"net/http"

	mgo "gopkg.in/mgo.v2"

	"configcenter/src/common"
	"configcenter/src/common/blog"
	"configcenter/src/common/util"
)

// by checking if bk_obj_id and bk_obj_name function parameter are valid net device object or not
// one of bk_obj_id and bk_obj_name can be empty and will return both bk_obj_id if no error
func (lgc *Logics) checkNetObject(objID string, objName string, pheader http.Header) (string, string, error) {
	defErr := lgc.Engine.CCErr.CreateDefaultCCErrorIf(util.GetLanguage(pheader))

	if "" == objName && "" == objID {
		blog.Errorf("check net device object, empty bk_obj_id and bk_obj_name")
		return "", "", defErr.Errorf(common.CCErrCommParamsLostField, common.BKObjIDField)
	}

	objCond := map[string]interface{}{
		common.BKOwnerIDField:          util.GetOwnerID(pheader),
		common.BKClassificationIDField: common.BKNetwork}

	if "" != objName {
		objCond[common.BKObjNameField] = objName
	}
	if "" != objID {
		objCond[common.BKObjIDField] = objID
	}

	objResult, err := lgc.CoreAPI.ObjectController().Meta().SelectObjects(context.Background(), pheader, objCond)
	if nil != err {
		blog.Errorf("check net device object, get net device object fail, error: %v, condition [%#v]", err, objCond)
		return "", "", defErr.Errorf(common.CCErrObjectSelectInstFailed)
	}

	if !objResult.Result {
		blog.Errorf("check net device object, errors: %s, condition [%#v]", objResult.ErrMsg, objCond)
		return "", "", defErr.Errorf(objResult.Code)
	}

	if nil == objResult.Data || 0 == len(objResult.Data) {
		blog.Errorf("check net device object, device object is not exist, condition [%#v]", objCond)
		return "", "", defErr.Errorf(common.CCErrCollectObjIDNotNetDevice)
	}

	return objResult.Data[0].ObjectID, objResult.Data[0].ObjectName, nil
}

// by checking if bk_property_id and bk_property_name function parameter are valid net device object property or not
// one of bk_property_id and bk_property_name can be empty and will return bk_property_id value if no error
func (lgc *Logics) checkNetObjectProperty(pheader http.Header, netDeviceObjID, propertyID, propertyName string) (string, error) {
	defErr := lgc.Engine.CCErr.CreateDefaultCCErrorIf(util.GetLanguage(pheader))

	if "" == netDeviceObjID {
		blog.Errorf("check net device object, empty bk_obj_id")
		return "", defErr.Errorf(common.CCErrCommParamsLostField, common.BKObjIDField)
	}

	if "" == propertyName && "" == propertyID {
		blog.Errorf("check net device object, empty bk_property_id and bk_property_name")
		return "", defErr.Errorf(common.CCErrCommParamsLostField, common.BKPropertyIDField)
	}

	propertyCond := map[string]interface{}{
		common.BKOwnerIDField: util.GetOwnerID(pheader),
		common.BKObjIDField:   netDeviceObjID}

	if "" != propertyName {
		propertyCond[common.BKPropertyNameField] = propertyName
	}
	if "" != propertyID {
		propertyCond[common.BKPropertyIDField] = propertyID
	}

	attrResult, err := lgc.CoreAPI.ObjectController().Meta().SelectObjectAttWithParams(context.Background(), pheader, propertyCond)
	if nil != err {
		blog.Errorf("get object attribute fail, error: %v, condition [%#v]", err, propertyCond)
		if mgo.ErrNotFound == err {
			return "", defErr.Errorf(common.CCErrCollectNetDeviceObjPropertyNotExist)
		}
		return "", defErr.Errorf(common.CCErrTopoObjectAttributeSelectFailed)
	}
	if !attrResult.Result {
		blog.Errorf("check net device object property, errors: %s", attrResult.ErrMsg)
		return "", defErr.Errorf(attrResult.Code)
	}

	if nil == attrResult.Data || 0 == len(attrResult.Data) {
		blog.Errorf("check net device object property, property is not exist, condition [%#v]", propertyCond)
		return "", defErr.Errorf(common.CCErrCollectNetDeviceObjPropertyNotExist)
	}

	return attrResult.Data[0].PropertyID, nil
}

// by checking if bk_device_id and bk_device_name function parameter are valid net device or not
// one of bk_device_id and bk_device_name can be empty and will return bk_device_id and bk_obj_id value if no error
// bk_obj_id is used to check property
func (lgc *Logics) checkNetDeviceExist(pheader http.Header, deviceID int64, deviceName string) (int64, string, error) {
	defErr := lgc.Engine.CCErr.CreateDefaultCCErrorIf(util.GetLanguage(pheader))

	if "" == deviceName && 0 == deviceID {
		blog.Errorf("check net device exist fail, empty device_id and device_name")
		return 0, "", defErr.Errorf(common.CCErrCommParamsLostField, common.BKDeviceIDField)
	}

	deviceCond := map[string]interface{}{common.BKOwnerIDField: util.GetOwnerID(pheader)}

	if "" != deviceName {
		deviceCond[common.BKDeviceNameField] = deviceName
	}
	if 0 != deviceID {
		deviceCond[common.BKDeviceIDField] = deviceID
	}

	attrResult := map[string]interface{}{}
	if err := lgc.Instance.GetOneByCondition(common.BKTableNameNetcollectDevice,
		[]string{common.BKDeviceIDField, common.BKObjIDField},
		deviceCond, &attrResult); nil != err {

		blog.Errorf("check net device exist fail, error: %v, condition [%#v]", err, deviceCond)
		if mgo.ErrNotFound == err {
			return 0, "", defErr.Errorf(common.CCErrCollectNetDeviceGetFail)
		}
		return 0, "", err
	}

	return attrResult[common.BKDeviceIDField].(int64), attrResult[common.BKObjIDField].(string), nil
}
