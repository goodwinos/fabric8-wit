package controller_test

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fabric8-services/fabric8-wit/account"
	"github.com/fabric8-services/fabric8-wit/app"
	"github.com/fabric8-services/fabric8-wit/app/test"
	"github.com/fabric8-services/fabric8-wit/application"
	"github.com/fabric8-services/fabric8-wit/area"
	. "github.com/fabric8-services/fabric8-wit/controller"
	"github.com/fabric8-services/fabric8-wit/errors"
	"github.com/fabric8-services/fabric8-wit/gormapplication"
	"github.com/fabric8-services/fabric8-wit/gormsupport"
	"github.com/fabric8-services/fabric8-wit/gormtestsupport"
	"github.com/fabric8-services/fabric8-wit/iteration"
	"github.com/fabric8-services/fabric8-wit/space"
	testsupport "github.com/fabric8-services/fabric8-wit/test"
	tf "github.com/fabric8-services/fabric8-wit/test/testfixture"
	"github.com/fabric8-services/fabric8-wit/workitem"

	"context"

	"github.com/goadesign/goa"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type TestIterationREST struct {
	gormtestsupport.DBTestSuite
	db      *gormapplication.GormDB
	testDir string
}

func TestRunIterationREST(t *testing.T) {
	// given
	suite.Run(t, &TestIterationREST{DBTestSuite: gormtestsupport.NewDBTestSuite("../config.yaml")})
}

func (rest *TestIterationREST) SetupTest() {
	rest.DBTestSuite.SetupTest()
	rest.db = gormapplication.NewGormDB(rest.DB)
	rest.testDir = filepath.Join("test-files", "iteration")
}

func (rest *TestIterationREST) SecuredController() (*goa.Service, *IterationController) {
	svc := testsupport.ServiceAsUser("Iteration-Service", testsupport.TestIdentity)
	return svc, NewIterationController(svc, rest.db, rest.Configuration)
}

func (rest *TestIterationREST) SecuredControllerWithIdentity(idn *account.Identity) (*goa.Service, *IterationController) {
	svc := testsupport.ServiceAsUser("Iteration-Service", *idn)
	return svc, NewIterationController(svc, rest.db, rest.Configuration)
}

func (rest *TestIterationREST) UnSecuredController() (*goa.Service, *IterationController) {
	svc := goa.New("Iteration-Service")
	return svc, NewIterationController(svc, rest.db, rest.Configuration)
}

func (rest *TestIterationREST) TestCreateChildIteration() {
	resetFn := rest.DisableGormCallbacks()
	defer resetFn()

	rest.T().Run("success - create child iteration", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.CreateWorkItemEnvironment(),
			tf.Iterations(2,
				tf.SetIterationNames("root iteration", "child iteration"),
				tf.PlaceIterationUnderRootIteration()))
		name := "Sprint #21"
		childItr := fxt.IterationByName("child iteration")
		ci := getChildIterationPayload(&name)
		startAt, err := time.Parse(time.RFC3339, "2016-11-04T15:08:41+00:00")
		require.Nil(t, err)
		endAt, err := time.Parse(time.RFC3339, "2016-11-25T15:08:41+00:00")
		require.Nil(t, err)
		ci.Data.Attributes.StartAt = &startAt
		ci.Data.Attributes.EndAt = &endAt
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		// when
		_, created := test.CreateChildIterationCreated(t, svc.Context, svc, ctrl, childItr.ID.String(), ci)
		// then
		require.NotNil(t, created)
		compareWithGoldenUUIDAgnostic(t, filepath.Join(rest.testDir, "create", "ok_create_child.golden.json"), created)
	})
	rest.T().Run("success - create child iteration with ID in request payload", func(t *testing.T) {
		// given
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.CreateWorkItemEnvironment(),
			tf.Iterations(2,
				tf.SetIterationNames("root iteration", "child iteration"),
			))
		name := "Sprint #21"
		childItr := fxt.IterationByName("child iteration")
		ci := getChildIterationPayload(&name)
		id := uuid.NewV4()
		ci.Data.ID = &id // set different ID and it must be ignoed by controller
		startAt, err := time.Parse(time.RFC3339, "2016-11-04T15:08:41+00:00")
		require.Nil(t, err)
		endAt, err := time.Parse(time.RFC3339, "2016-11-25T15:08:41+00:00")
		require.Nil(t, err)
		ci.Data.Attributes.StartAt = &startAt
		ci.Data.Attributes.EndAt = &endAt
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		// when
		_, created := test.CreateChildIterationCreated(t, svc.Context, svc, ctrl, childItr.ID.String(), ci)
		// then
		require.NotNil(t, created)
		compareWithGoldenUUIDAgnostic(t, filepath.Join(rest.testDir, "create", "ok_create_child_ID_paylod.golden.json"), created)
		require.Equal(t, *ci.Data.ID, *created.Data.ID)
	})

	rest.T().Run("forbidden - only space owener can create child iteration", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.Identities(2, tf.SetIdentityUsernames("space owner", "other user")),
			tf.Areas(1), tf.Iterations(1))
		name := "Sprint #21"
		ci := getChildIterationPayload(&name)
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.IdentityByUsername("other user"))
		_, jerrs := test.CreateChildIterationForbidden(t, svc.Context, svc, ctrl, fxt.Iterations[0].ID.String(), ci)
		compareWithGoldenUUIDAgnostic(t, filepath.Join(rest.testDir, "create", "forbidden_other_user.golden.json"), jerrs)
	})

	rest.T().Run("fail - create same child iteration conflict", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB, tf.Identities(1), tf.Areas(1),
			tf.Iterations(2, func(fxt *tf.TestFixture, idx int) error {
				if idx == 1 {
					fxt.Iterations[idx].MakeChildOf(*fxt.Iterations[0])
				}
				return nil
			}))
		name := fxt.Iterations[1].Name
		ci := getChildIterationPayload(&name)
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		_, jerrs := test.CreateChildIterationConflict(t, svc.Context, svc, ctrl, fxt.Iterations[0].ID.String(), ci)
		compareWithGoldenUUIDAgnostic(t, filepath.Join(rest.testDir, "create", "conflict_for_same_name.golden.json"), jerrs)
	})

	rest.T().Run("fail - create child iteration missing name", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB, tf.Identities(1), tf.Areas(1), tf.Iterations(1))
		ci := getChildIterationPayload(nil)
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		_, jerrs := test.CreateChildIterationBadRequest(t, svc.Context, svc, ctrl, fxt.Iterations[0].ID.String(), ci)
		compareWithGoldenUUIDAgnostic(t, filepath.Join(rest.testDir, "create", "bad_request_missing_name.golden.json"), jerrs)
	})

	rest.T().Run("fail - create child missing parent", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB, tf.Identities(1), tf.Areas(1), tf.Iterations(1))
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		name := "Sprint #21"
		ci := getChildIterationPayload(&name)
		_, jerrs := test.CreateChildIterationNotFound(t, svc.Context, svc, ctrl, uuid.NewV4().String(), ci)
		ignoreString := "IGNORE_ME"
		jerrs.Errors[0].ID = &ignoreString
		compareWithGoldenUUIDAgnostic(t, filepath.Join(rest.testDir, "create", "bad_request_unknown_parent.golden.json"), jerrs)
	})

	rest.T().Run("unauthorized - create child iteration with unauthorized user", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB, tf.Identities(1), tf.Iterations(1))
		name := "Sprint #21"
		ci := getChildIterationPayload(&name)
		svc, ctrl := rest.UnSecuredController()
		_, jerrs := test.CreateChildIterationUnauthorized(t, svc.Context, svc, ctrl, fxt.Iterations[0].ID.String(), ci)
		ignoreString := "IGNORE_ME"
		jerrs.Errors[0].ID = &ignoreString
		compareWithGoldenUUIDAgnostic(t, filepath.Join(rest.testDir, "create", "unauthorized.golden.json"), jerrs)
	})
}

func (rest *TestIterationREST) TestFailValidationIterationNameLength() {
	// given
	_, _, _, _, parent := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	_, err := rest.db.Iterations().Root(context.Background(), parent.SpaceID)
	require.Nil(rest.T(), err)
	ci := getChildIterationPayload(&testsupport.TestOversizedNameObj)

	err = ci.Validate()
	// Validate payload function returns an error
	assert.NotNil(rest.T(), err)
	assert.Contains(rest.T(), err.Error(), "length of type.name must be less than or equal to 62")
}

func (rest *TestIterationREST) TestFailValidationIterationNameStartWith() {
	// given
	_, _, _, _, parent := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	_, err := rest.db.Iterations().Root(context.Background(), parent.SpaceID)
	require.Nil(rest.T(), err)
	name := "_Sprint #21"
	ci := getChildIterationPayload(&name)

	err = ci.Validate()
	// Validate payload function returns an error
	assert.NotNil(rest.T(), err)
	assert.Contains(rest.T(), err.Error(), "type.name must match the regexp")
}

func (rest *TestIterationREST) TestShowIterationOK() {
	// given
	_, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	svc, ctrl := rest.SecuredController()
	// when
	_, created := test.ShowIterationOK(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), nil, nil)
	// then
	assertIterationLinking(rest.T(), created.Data)
	require.NotNil(rest.T(), created.Data.Relationships.Workitems.Meta)
	assert.Equal(rest.T(), 0, created.Data.Relationships.Workitems.Meta[KeyTotalWorkItems])
	assert.Equal(rest.T(), 0, created.Data.Relationships.Workitems.Meta[KeyClosedWorkItems])
}

func (rest *TestIterationREST) TestShowIterationOKUsingExpiredIfModifiedSinceHeader() {
	// given
	_, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	svc, ctrl := rest.SecuredController()
	// when
	ifModifiedSinceHeader := app.ToHTTPTime(itr.UpdatedAt.Add(-1 * time.Hour))
	_, created := test.ShowIterationOK(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), &ifModifiedSinceHeader, nil)
	// then
	assertIterationLinking(rest.T(), created.Data)
	require.NotNil(rest.T(), created.Data.Relationships.Workitems.Meta)
	assert.Equal(rest.T(), 0, created.Data.Relationships.Workitems.Meta[KeyTotalWorkItems])
	assert.Equal(rest.T(), 0, created.Data.Relationships.Workitems.Meta[KeyClosedWorkItems])
}

func (rest *TestIterationREST) TestShowIterationOKUsingExpiredIfNoneMatchHeader() {
	// given
	_, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	svc, ctrl := rest.SecuredController()
	// when
	ifNoneMatch := "foo"
	_, created := test.ShowIterationOK(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), nil, &ifNoneMatch)
	// then
	assertIterationLinking(rest.T(), created.Data)
	require.NotNil(rest.T(), created.Data.Relationships.Workitems.Meta)
	assert.Equal(rest.T(), 0, created.Data.Relationships.Workitems.Meta[KeyTotalWorkItems])
	assert.Equal(rest.T(), 0, created.Data.Relationships.Workitems.Meta[KeyClosedWorkItems])
}

func (rest *TestIterationREST) TestShowIterationNotModifiedUsingIfModifiedSinceHeader() {
	// given
	_, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	svc, ctrl := rest.SecuredController()
	// when/then
	rest.T().Log("Iteration:", itr, " updatedAt: ", itr.UpdatedAt)
	ifModifiedSinceHeader := app.ToHTTPTime(itr.UpdatedAt)
	test.ShowIterationNotModified(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), &ifModifiedSinceHeader, nil)
}

func (rest *TestIterationREST) TestShowIterationNotModifiedUsingIfNoneMatchHeader() {
	// given
	_, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	svc, ctrl := rest.SecuredController()
	// when/then
	ifNoneMatch := app.GenerateEntityTag(itr)
	test.ShowIterationNotModified(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), nil, &ifNoneMatch)
}

func (rest *TestIterationREST) createWorkItem(parentSpace space.Space) workitem.WorkItem {
	var wi *workitem.WorkItem
	err := application.Transactional(gormapplication.NewGormDB(rest.DB), func(app application.Application) error {
		fields := map[string]interface{}{
			workitem.SystemTitle: "Test Item",
			workitem.SystemState: "new",
		}
		w, err := app.WorkItems().Create(context.Background(), parentSpace.ID, workitem.SystemBug, fields, parentSpace.OwnerID)
		wi = w
		return err
	})
	require.Nil(rest.T(), err)
	return *wi
}

func (rest *TestIterationREST) TestShowIterationModifiedUsingIfModifiedSinceHeaderAfterWorkItemLinking() {
	// given
	parentSpace, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	svc, ctrl := rest.SecuredController()
	rest.T().Logf("Iteration: %s: updatedAt: %s", itr.ID.String(), itr.UpdatedAt.String())
	ifModifiedSinceHeader := app.ToHTTPTime(itr.UpdatedAt)
	testWI := rest.createWorkItem(parentSpace)
	testWI.Fields[workitem.SystemIteration] = itr.ID.String()
	// need to wait at least 1s because HTTP date time does not include microseconds, hence `Last-Modified` vs `If-Modified-Since` comparison may fail
	time.Sleep(1 * time.Second)
	err := application.Transactional(rest.db, func(app application.Application) error {
		_, err := app.WorkItems().Save(context.Background(), parentSpace.ID, testWI, parentSpace.OwnerID)
		return err
	})
	require.Nil(rest.T(), err)
	// when/then
	test.ShowIterationOK(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), &ifModifiedSinceHeader, nil)
}

func (rest *TestIterationREST) TestShowIterationModifiedUsingIfModifiedSinceHeaderAfterWorkItemUnlinking() {
	// given
	parentSpace, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	svc, ctrl := rest.SecuredController()
	rest.T().Logf("Iteration: %s: updatedAt: %s", itr.ID.String(), itr.UpdatedAt.String())
	testWI := rest.createWorkItem(parentSpace)
	testWI.Fields[workitem.SystemIteration] = itr.ID.String()
	// need to wait at least 1s because HTTP date time does not include microseconds, hence `Last-Modified` vs `If-Modified-Since` comparison may fail
	time.Sleep(1 * time.Second)
	var updatedWI *workitem.WorkItem
	err := application.Transactional(rest.db, func(app application.Application) error {
		w, err := app.WorkItems().Save(context.Background(), parentSpace.ID, testWI, parentSpace.OwnerID)
		updatedWI = w
		return err
	})
	require.Nil(rest.T(), err)
	testWI = *updatedWI
	// read the iteration to compute its current `If-Modified-Since` value
	var updatedItr *iteration.Iteration
	err = application.Transactional(rest.db, func(app application.Application) error {
		i, err := app.Iterations().Load(context.Background(), itr.ID)
		updatedItr = i
		return err
	})
	ifModifiedSinceHeader := app.ToHTTPTime(updatedItr.GetLastModified())
	// now, unlink the work item from the iteration
	// need to wait at least 1s because HTTP date time does not include microseconds, hence `Last-Modified` vs `If-Modified-Since` comparison may fail
	delete(testWI.Fields, workitem.SystemIteration)
	time.Sleep(1 * time.Second)
	err = application.Transactional(rest.db, func(app application.Application) error {
		_, err := app.WorkItems().Save(context.Background(), parentSpace.ID, testWI, parentSpace.OwnerID)
		return err
	})
	require.Nil(rest.T(), err)
	// when/then
	test.ShowIterationOK(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), &ifModifiedSinceHeader, nil)
}

func (rest *TestIterationREST) TestShowIterationModifiedUsingIfNoneMatchHeaderAfterWorkItemLinking() {
	// given
	parentSpace, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	svc, ctrl := rest.SecuredController()
	ifNoneMatch := app.GenerateEntityTag(itr)
	// now, create and attach a work item to the iteration
	testWI := rest.createWorkItem(parentSpace)
	testWI.Fields[workitem.SystemIteration] = itr.ID.String()
	err := application.Transactional(rest.db, func(app application.Application) error {
		_, err := app.WorkItems().Save(context.Background(), parentSpace.ID, testWI, parentSpace.OwnerID)
		return err
	})
	require.Nil(rest.T(), err)
	// when/then
	test.ShowIterationOK(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), nil, &ifNoneMatch)
}

func (rest *TestIterationREST) TestShowIterationModifiedUsingIfNoneMatchHeaderAfterWorkItemUnlinking() {
	// given
	parentSpace, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	svc, ctrl := rest.SecuredController()
	rest.T().Logf("Iteration: %s: updatedAt: %s", itr.ID.String(), itr.UpdatedAt.String())
	testWI := rest.createWorkItem(parentSpace)
	testWI.Fields[workitem.SystemIteration] = itr.ID.String()
	// need to wait at least 1s because HTTP date time does not include microseconds, hence `Last-Modified` vs `If-Modified-Since` comparison may fail
	time.Sleep(1 * time.Second)
	var updatedWI *workitem.WorkItem
	err := application.Transactional(rest.db, func(app application.Application) error {
		w, err := app.WorkItems().Save(context.Background(), parentSpace.ID, testWI, parentSpace.OwnerID)
		updatedWI = w
		return err
	})
	require.Nil(rest.T(), err)
	testWI = *updatedWI
	// read the iteration to compute its current `If-None-Match` value
	var updatedItr *iteration.Iteration
	err = application.Transactional(rest.db, func(app application.Application) error {
		i, err := app.Iterations().Load(context.Background(), itr.ID)
		updatedItr = i
		return err
	})
	ifNoneMatch := app.GenerateEntityTag(*updatedItr)
	// now, unlink the work item from the iteration
	// need to wait at least 1s because HTTP date time does not include microseconds, hence `Last-Modified` vs `If-Modified-Since` comparison may fail
	delete(testWI.Fields, workitem.SystemIteration)
	time.Sleep(1 * time.Second)
	err = application.Transactional(rest.db, func(app application.Application) error {
		_, err := app.WorkItems().Save(context.Background(), parentSpace.ID, testWI, parentSpace.OwnerID)
		return err
	})
	require.Nil(rest.T(), err)
	// when/then
	test.ShowIterationOK(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), nil, &ifNoneMatch)
}

func (rest *TestIterationREST) TestFailShowIterationMissing() {
	// given
	svc, ctrl := rest.SecuredController()
	// when/then
	test.ShowIterationNotFound(rest.T(), svc.Context, svc, ctrl, uuid.NewV4().String(), nil, nil)
}

func (rest *TestIterationREST) TestSuccessUpdateIteration() {
	// given
	sp, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	newName := "Sprint 1001"
	newDesc := "New Description"
	payload := app.UpdateIterationPayload{
		Data: &app.Iteration{
			Attributes: &app.IterationAttributes{
				Name:        &newName,
				Description: &newDesc,
			},
			ID:   &itr.ID,
			Type: iteration.APIStringTypeIteration,
		},
	}
	owner, errIdn := rest.db.Identities().Load(context.Background(), sp.OwnerID)
	require.Nil(rest.T(), errIdn)
	svc, ctrl := rest.SecuredControllerWithIdentity(owner)
	// when
	_, updated := test.UpdateIterationOK(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), &payload)
	// then
	assert.Equal(rest.T(), newName, *updated.Data.Attributes.Name)
	assert.Equal(rest.T(), newDesc, *updated.Data.Attributes.Description)
	require.NotNil(rest.T(), updated.Data.Relationships.Workitems.Meta)
	assert.Equal(rest.T(), 0, updated.Data.Relationships.Workitems.Meta[KeyTotalWorkItems])
	assert.Equal(rest.T(), 0, updated.Data.Relationships.Workitems.Meta[KeyClosedWorkItems])

	// try update using some other user
	otherIdentity := &account.Identity{
		Username:     "non-space-owner-identity",
		ProviderType: account.KeycloakIDP,
	}
	errInCreateOther := rest.db.Identities().Create(context.Background(), otherIdentity)
	require.Nil(rest.T(), errInCreateOther)
	svc, ctrl = rest.SecuredControllerWithIdentity(otherIdentity)
	test.UpdateIterationForbidden(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), &payload)
}

func (rest *TestIterationREST) TestSuccessUpdateIterationWithWICounts() {
	// given
	sp, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	newName := "Sprint 1001"
	newDesc := "New Description"
	payload := app.UpdateIterationPayload{
		Data: &app.Iteration{
			Attributes: &app.IterationAttributes{
				Name:        &newName,
				Description: &newDesc,
			},
			ID:   &itr.ID,
			Type: iteration.APIStringTypeIteration,
		},
	}
	// add WI to this iteration and test counts in the response of update iteration API
	testIdentity, err := testsupport.CreateTestIdentity(rest.DB, "TestSuccessUpdateIterationWithWICounts user", "test provider")
	require.Nil(rest.T(), err)
	wirepo := workitem.NewWorkItemRepository(rest.DB)
	req := &http.Request{Host: "localhost"}
	params := url.Values{}
	ctx := goa.NewContext(context.Background(), nil, req, params)

	for i := 0; i < 4; i++ {
		wi, err := wirepo.Create(
			ctx, itr.SpaceID, workitem.SystemBug,
			map[string]interface{}{
				workitem.SystemTitle:     fmt.Sprintf("New issue #%d", i),
				workitem.SystemState:     workitem.SystemStateNew,
				workitem.SystemIteration: itr.ID.String(),
			}, testIdentity.ID)
		require.NotNil(rest.T(), wi)
		require.Nil(rest.T(), err)
		require.NotNil(rest.T(), wi)
	}
	for i := 0; i < 5; i++ {
		wi, err := wirepo.Create(
			ctx, itr.SpaceID, workitem.SystemBug,
			map[string]interface{}{
				workitem.SystemTitle:     fmt.Sprintf("Closed issue #%d", i),
				workitem.SystemState:     workitem.SystemStateClosed,
				workitem.SystemIteration: itr.ID.String(),
			}, testIdentity.ID)
		require.NotNil(rest.T(), wi)
		require.Nil(rest.T(), err)
		require.NotNil(rest.T(), wi)
	}
	owner, errIdn := rest.db.Identities().Load(context.Background(), sp.OwnerID)
	require.Nil(rest.T(), errIdn)
	svc, ctrl := rest.SecuredControllerWithIdentity(owner)
	// when
	_, updated := test.UpdateIterationOK(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), &payload)
	// then
	require.NotNil(rest.T(), updated)
	assert.Equal(rest.T(), newName, *updated.Data.Attributes.Name)
	assert.Equal(rest.T(), newDesc, *updated.Data.Attributes.Description)
	require.NotNil(rest.T(), updated.Data.Relationships.Workitems.Meta)
	assert.Equal(rest.T(), 9, updated.Data.Relationships.Workitems.Meta[KeyTotalWorkItems])
	assert.Equal(rest.T(), 5, updated.Data.Relationships.Workitems.Meta[KeyClosedWorkItems])
}

func (rest *TestIterationREST) TestFailUpdateIterationNotFound() {
	// given
	_, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	itr.ID = uuid.NewV4()
	payload := app.UpdateIterationPayload{
		Data: &app.Iteration{
			Attributes: &app.IterationAttributes{},
			ID:         &itr.ID,
			Type:       iteration.APIStringTypeIteration,
		},
	}
	svc, ctrl := rest.SecuredController()
	// when/then
	test.UpdateIterationNotFound(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), &payload)
}

func (rest *TestIterationREST) TestFailUpdateIterationUnauthorized() {
	// given
	_, _, _, _, itr := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	payload := app.UpdateIterationPayload{
		Data: &app.Iteration{
			Attributes: &app.IterationAttributes{},
			ID:         &itr.ID,
			Type:       iteration.APIStringTypeIteration,
		},
	}
	svc, ctrl := rest.UnSecuredController()
	// when/then
	test.UpdateIterationUnauthorized(rest.T(), svc.Context, svc, ctrl, itr.ID.String(), &payload)
}

func (rest *TestIterationREST) TestIterationStateTransitions() {
	// given
	sp, _, _, _, itr1 := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	assert.Equal(rest.T(), iteration.StateNew, itr1.State)
	startState := iteration.StateStart
	payload := app.UpdateIterationPayload{
		Data: &app.Iteration{
			Attributes: &app.IterationAttributes{
				State: startState.StringPtr(),
			},
			ID:   &itr1.ID,
			Type: iteration.APIStringTypeIteration,
		},
	}
	owner, errIdn := rest.db.Identities().Load(context.Background(), sp.OwnerID)
	require.Nil(rest.T(), errIdn)
	svc, ctrl := rest.SecuredControllerWithIdentity(owner)
	_, updated := test.UpdateIterationOK(rest.T(), svc.Context, svc, ctrl, itr1.ID.String(), &payload)
	assert.Equal(rest.T(), startState.String(), *updated.Data.Attributes.State)
	// create another iteration in same space and then change State to start
	itr2 := iteration.Iteration{
		Name:    "Spring 123",
		SpaceID: itr1.SpaceID,
		Path:    itr1.Path,
	}
	err := rest.db.Iterations().Create(context.Background(), &itr2)
	require.Nil(rest.T(), err)
	payload2 := app.UpdateIterationPayload{
		Data: &app.Iteration{
			Attributes: &app.IterationAttributes{
				State: startState.StringPtr(),
			},
			ID:   &itr2.ID,
			Type: iteration.APIStringTypeIteration,
		},
	}
	test.UpdateIterationBadRequest(rest.T(), svc.Context, svc, ctrl, itr2.ID.String(), &payload2)
	// now close first iteration
	closeState := iteration.StateClose
	payload.Data.Attributes.State = closeState.StringPtr()
	_, updated = test.UpdateIterationOK(rest.T(), svc.Context, svc, ctrl, itr1.ID.String(), &payload)
	assert.Equal(rest.T(), closeState.String(), *updated.Data.Attributes.State)
	// try to start iteration 2 now
	_, updated2 := test.UpdateIterationOK(rest.T(), svc.Context, svc, ctrl, itr2.ID.String(), &payload2)
	assert.Equal(rest.T(), startState.String(), *updated2.Data.Attributes.State)
}

func (rest *TestIterationREST) TestRootIterationCanNotStart() {
	// given
	sp, _, _, _, itr1 := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	var ri *iteration.Iteration
	err := application.Transactional(rest.db, func(app application.Application) error {
		repo := app.Iterations()
		var err error
		ri, err = repo.Root(context.Background(), itr1.SpaceID)
		return err
	})
	require.Nil(rest.T(), err)
	require.NotNil(rest.T(), ri)

	startState := iteration.StateStart
	payload := app.UpdateIterationPayload{
		Data: &app.Iteration{
			Attributes: &app.IterationAttributes{
				State: startState.StringPtr(),
			},
			ID:   &ri.ID,
			Type: iteration.APIStringTypeIteration,
		},
	}
	owner, errIdn := rest.db.Identities().Load(context.Background(), sp.OwnerID)
	require.Nil(rest.T(), errIdn)
	svc, ctrl := rest.SecuredControllerWithIdentity(owner)
	test.UpdateIterationBadRequest(rest.T(), svc.Context, svc, ctrl, ri.ID.String(), &payload)
}

func (rest *TestIterationREST) createIterations() (*app.IterationSingle, *account.Identity) {
	sp, _, _, _, parent := createSpaceAndRootAreaAndIterations(rest.T(), rest.db)
	_, err := rest.db.Iterations().Root(context.Background(), parent.SpaceID)
	require.Nil(rest.T(), err)
	parentID := parent.ID
	name := testsupport.CreateRandomValidTestName("Iteration-")
	ci := getChildIterationPayload(&name)
	owner, err := rest.db.Identities().Load(context.Background(), sp.OwnerID)
	require.Nil(rest.T(), err)
	svc, ctrl := rest.SecuredControllerWithIdentity(owner)
	// when
	_, created := test.CreateChildIterationCreated(rest.T(), svc.Context, svc, ctrl, parentID.String(), ci)
	// then
	require.NotNil(rest.T(), created)
	return created, owner
}

// TestIterationActiveInTimeframe tests iteration should be active when it is in timeframe
func (rest *TestIterationREST) TestIterationActiveInTimeframe() {
	itr1, _ := rest.createIterations()
	assert.Equal(rest.T(), iteration.IterationNotActive, *itr1.Data.Attributes.UserActive)
	assert.Equal(rest.T(), iteration.IterationActive, *itr1.Data.Attributes.ActiveStatus) // iteration falls in timeframe, so iteration is active
}

// TestIterationNotActiveInTimeframe tests iteration should not be active when it is outside the timeframe
func (rest *TestIterationREST) TestIterationNotActiveInTimeframe() {
	itr1, owner := rest.createIterations()
	startDate := time.Date(2017, 5, 17, 00, 00, 00, 00, time.UTC)
	endDate := time.Date(2017, 6, 17, 00, 00, 00, 00, time.UTC)
	payload := app.UpdateIterationPayload{
		Data: &app.Iteration{
			Attributes: &app.IterationAttributes{
				StartAt: &startDate,
				EndAt:   &endDate,
			},
			ID:   itr1.Data.ID,
			Type: iteration.APIStringTypeIteration,
		},
	}
	owner, errIdn := rest.db.Identities().Load(context.Background(), owner.ID)
	require.Nil(rest.T(), errIdn)
	svc, ctrl := rest.SecuredControllerWithIdentity(owner)
	_, updated := test.UpdateIterationOK(rest.T(), svc.Context, svc, ctrl, itr1.Data.ID.String(), &payload)
	assert.Equal(rest.T(), iteration.IterationNotActive, *updated.Data.Attributes.ActiveStatus) // iteration doesnot fall in timeframe, so iteration is not active
}

// TestIterationActivatedByUser tests iteration should always be active when user sets it to active
func (rest *TestIterationREST) TestIterationActivatedByUser() {
	itr1, owner := rest.createIterations()
	userActive := true
	payload := app.UpdateIterationPayload{
		Data: &app.Iteration{
			Attributes: &app.IterationAttributes{
				UserActive: &userActive,
			},
			ID:   itr1.Data.ID,
			Type: iteration.APIStringTypeIteration,
		},
	}
	owner, errIdn := rest.db.Identities().Load(context.Background(), owner.ID)
	require.Nil(rest.T(), errIdn)
	svc, ctrl := rest.SecuredControllerWithIdentity(owner)
	_, updated := test.UpdateIterationOK(rest.T(), svc.Context, svc, ctrl, itr1.Data.ID.String(), &payload)
	assert.Equal(rest.T(), iteration.IterationActive, *updated.Data.Attributes.ActiveStatus) // iteration doesnot fall in timeframe yet userActive is true so iteration is active
}

func getChildIterationPayload(name *string) *app.CreateChildIterationPayload {
	start := time.Now()
	end := start.Add(time.Hour * (24 * 8 * 3))

	itType := iteration.APIStringTypeIteration
	desc := "Some description"
	return &app.CreateChildIterationPayload{
		Data: &app.Iteration{
			Type: itType,
			Attributes: &app.IterationAttributes{
				Name:        name,
				Description: &desc,
				StartAt:     &start,
				EndAt:       &end,
			},
		},
	}
}

// following helper function creates a space , root area, root iteration for that space.
// Also creates a new iteration and new area in the same space
func createSpaceAndRootAreaAndIterations(t *testing.T, db application.DB) (space.Space, area.Area, iteration.Iteration, area.Area, iteration.Iteration) {
	var (
		spaceObj          space.Space
		rootAreaObj       area.Area
		rootIterationObj  iteration.Iteration
		otherIterationObj iteration.Iteration
		otherAreaObj      area.Area
	)

	application.Transactional(db, func(app application.Application) error {
		owner := &account.Identity{
			Username:     "new-space-owner-identity",
			ProviderType: account.KeycloakIDP,
		}
		errCreateOwner := app.Identities().Create(context.Background(), owner)
		require.Nil(t, errCreateOwner)
		spaceObj = space.Space{
			Name:    testsupport.CreateRandomValidTestName("CreateSpaceAndRootAreaAndIterations-"),
			OwnerID: owner.ID,
		}
		_, err := app.Spaces().Create(context.Background(), &spaceObj)
		require.Nil(t, err)
		// create the root area
		rootAreaObj = area.Area{
			Name:    spaceObj.Name,
			SpaceID: spaceObj.ID,
		}
		err = app.Areas().Create(context.Background(), &rootAreaObj)
		require.Nil(t, err)
		// above space should have a root iteration for itself
		rootIterationObj = iteration.Iteration{
			Name:    spaceObj.Name,
			SpaceID: spaceObj.ID,
		}
		err = app.Iterations().Create(context.Background(), &rootIterationObj)
		require.Nil(t, err)
		start := time.Now()
		end := start.Add(time.Hour * (24 * 8 * 3))
		iterationName := "Sprint #2"
		otherIterationObj = iteration.Iteration{
			Lifecycle: gormsupport.Lifecycle{
				CreatedAt: spaceObj.CreatedAt,
				UpdatedAt: spaceObj.UpdatedAt,
			},
			Name:    iterationName,
			SpaceID: spaceObj.ID,
			StartAt: &start,
			EndAt:   &end,
			Path:    append(rootIterationObj.Path, rootIterationObj.ID),
		}
		err = app.Iterations().Create(context.Background(), &otherIterationObj)
		require.Nil(t, err)

		areaName := "Area #2"
		otherAreaObj = area.Area{
			Lifecycle: gormsupport.Lifecycle{
				CreatedAt: spaceObj.CreatedAt,
				UpdatedAt: spaceObj.UpdatedAt,
			},
			Name:    areaName,
			SpaceID: spaceObj.ID,
			Path:    append(rootAreaObj.Path, rootAreaObj.ID),
		}
		err = app.Areas().Create(context.Background(), &otherAreaObj)
		require.Nil(t, err)
		return nil
	})
	t.Log("Created space with ID=", spaceObj.ID.String(), "name=", spaceObj.Name)
	return spaceObj, rootAreaObj, rootIterationObj, otherAreaObj, otherIterationObj
}

func assertIterationLinking(t *testing.T, target *app.Iteration) {
	assert.NotNil(t, target.ID)
	assert.Equal(t, iteration.APIStringTypeIteration, target.Type)
	assert.NotNil(t, target.Links.Self)
	require.NotNil(t, target.Relationships)
	require.NotNil(t, target.Relationships.Space)
	require.NotNil(t, target.Relationships.Space.Links)
	require.NotNil(t, target.Relationships.Space.Links.Self)
	assert.True(t, strings.Contains(*target.Relationships.Space.Links.Self, "/api/spaces/"))
}

func assertChildIterationLinking(t *testing.T, target *app.Iteration) {
	assertIterationLinking(t, target)
	require.NotNil(t, target.Relationships)
	require.NotNil(t, target.Relationships.Parent)
	require.NotNil(t, target.Relationships.Parent.Links)
	require.NotNil(t, target.Relationships.Parent.Links.Self)
}

// TestIterationDelete tests iteration delete API
func (rest *TestIterationREST) TestIterationDelete() {
	rest.T().Run("forbidden - delete root iteration", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.Iterations(1, tf.SetIterationNames("root iteration")))
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		iterationToDelete := fxt.IterationByName("root iteration")
		test.DeleteIterationForbidden(t, svc.Context, svc, ctrl, iterationToDelete.ID)
	})

	rest.T().Run("success - delete one iteration", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.CreateWorkItemEnvironment(),
			tf.Iterations(2,
				tf.SetIterationNames("root iteration", "first iteration"),
			))
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		iterationToDelete := fxt.IterationByName("first iteration")
		test.DeleteIterationNoContent(t, svc.Context, svc, ctrl, iterationToDelete.ID)
		_, err := rest.db.Iterations().Load(svc.Context, iterationToDelete.ID)
		require.NotNil(t, err)
		require.IsType(t, errors.NotFoundError{}, err, "error was %v", err)
	})

	rest.T().Run("success - delete iteration subtree", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.Iterations(6,
				tf.SetIterationNames("root", "child 1", "child 1.2", "child 1.2.3", "child 1.2.3.4", "child 2"),
				func(fxt *tf.TestFixture, idx int) error {
					i := fxt.Iterations[idx]
					switch idx {
					case 1:
						i.MakeChildOf(*fxt.Iterations[0])
					case 2:
						i.MakeChildOf(*fxt.Iterations[1])
					case 3:
						i.MakeChildOf(*fxt.Iterations[2])
					case 4:
						i.MakeChildOf(*fxt.Iterations[3])
					case 5:
						i.MakeChildOf(*fxt.Iterations[0])
					}
					return nil
				}))
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		iterationToDelete := fxt.IterationByName("child 1")
		test.DeleteIterationNoContent(t, svc.Context, svc, ctrl, iterationToDelete.ID)
		// make sure all nested iterations are deleted
		deletedIterations := []*iteration.Iteration{
			fxt.IterationByName("child 1"),
			fxt.IterationByName("child 1.2"),
			fxt.IterationByName("child 1.2.3"),
			fxt.IterationByName("child 1.2.3.4"),
		}
		for _, i := range deletedIterations {
			_, err := rest.db.Iterations().Load(svc.Context, i.ID)
			require.NotNil(t, err)
			require.IsType(t, errors.NotFoundError{}, err, "error was %v", err)
		}
		// make sure other iterations are not touched
		iterationsShouldPresent := []*iteration.Iteration{
			fxt.IterationByName("root"),
			fxt.IterationByName("child 2"),
		}
		for _, i := range iterationsShouldPresent {
			_, err := rest.db.Iterations().Load(svc.Context, i.ID)
			require.Nil(t, err)
		}
	})

	rest.T().Run("forbidden - other user can not delete iteration", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.Identities(2, tf.SetIdentityUsernames("space owner", "other user")),
			tf.Iterations(1))
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.IdentityByUsername("other user"))
		test.DeleteIterationForbidden(t, svc.Context, svc, ctrl, fxt.Iterations[0].ID)
	})

	rest.T().Run("success - space owner can delete iteration", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB, tf.Iterations(2, func(fxt *tf.TestFixture, idx int) error {
			if idx == 1 {
				fxt.Iterations[idx].MakeChildOf(*fxt.Iterations[0])
			}
			return nil
		}))
		iterationToDelete := fxt.Iterations[1]                             // non-root iteration
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0]) // get the space owner
		test.DeleteIterationNoContent(t, svc.Context, svc, ctrl, iterationToDelete.ID)
		_, err := rest.db.Iterations().Load(svc.Context, iterationToDelete.ID)
		require.NotNil(t, err)
		require.IsType(t, errors.NotFoundError{}, err, "error was %v", err)
	})

	rest.T().Run("unauthorized - invalid user can not delete iteration", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB, tf.Iterations(1))
		svc, ctrl := rest.UnSecuredController()
		test.DeleteIterationUnauthorized(t, svc.Context, svc, ctrl, fxt.Iterations[0].ID)
	})

	rest.T().Run("success - update workitems for deleted iteration", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.Iterations(2, func(fxt *tf.TestFixture, idx int) error {
				if idx == 1 {
					fxt.Iterations[idx].MakeChildOf(*fxt.Iterations[0])
				}
				return nil
			}),
			tf.WorkItems(5, func(fxt *tf.TestFixture, idx int) error {
				wi := fxt.WorkItems[idx]
				wi.Fields[workitem.SystemIteration] = fxt.Iterations[1].ID.String()
				return nil
			}))
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		iterationToDelete := fxt.Iterations[1]
		test.DeleteIterationNoContent(t, svc.Context, svc, ctrl, iterationToDelete.ID)
		wis, err := rest.db.WorkItems().LoadByIteration(svc.Context, iterationToDelete.ID)
		require.Nil(t, err)
		assert.Empty(t, wis)
	})

	rest.T().Run("success - delete intermediate iteration", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.Iterations(3, func(fxt *tf.TestFixture, idx int) error {
				itr := fxt.Iterations[idx]
				switch idx {
				case 0:
					itr.Name = "root"
				case 1:
					itr.Name = "parent"
					itr.MakeChildOf(*fxt.Iterations[0])
				case 2:
					itr.Name = "child"
					itr.MakeChildOf(*fxt.Iterations[1])
				}
				return nil
			}),
			tf.WorkItems(6, func(fxt *tf.TestFixture, idx int) error {
				wi := fxt.WorkItems[idx]
				if idx < 3 {
					wi.Fields[workitem.SystemIteration] = fxt.Iterations[1].ID.String()
				} else {
					wi.Fields[workitem.SystemIteration] = fxt.Iterations[2].ID.String()
				}
				return nil
			}))
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		childIteration := fxt.IterationByName("child")
		test.DeleteIterationNoContent(t, svc.Context, svc, ctrl, childIteration.ID)
		wis, err := rest.db.WorkItems().LoadByIteration(svc.Context, childIteration.ID)
		require.Nil(t, err)
		assert.Empty(t, wis)

		// parent should get more 3 WI
		parentIteration := fxt.IterationByName("parent")
		wis, err = rest.db.WorkItems().LoadByIteration(svc.Context, parentIteration.ID)
		require.Nil(t, err)
		// first iteration already have 3 & 3 more from child iteration
		assert.Len(t, wis, 3+3)

		// verify that root iteration still does not have any WI
		rootIteration := fxt.IterationByName("root")
		wis, err = rest.db.WorkItems().LoadByIteration(svc.Context, rootIteration.ID)
		require.Nil(t, err)
		assert.Empty(t, wis)
	})

	// Following test creates the structure shown in diagram
	// root Iteration
	// |___________Iteration 1 (5 WI)
	// |                |___________Iteration 2 (5 WI)
	// |                                |___________Iteration 3 (5 WI)
	// |___________Iteration 4 (2 WI)
	//                     |___________Iteration 5 (3 WI)

	// then deletes iteration1 & iteration5 to verify the effect When iteration1
	// is deleted, iteration2 & iteration3 should also get deleted and 15 WIs
	// should be moved to root iteration when iteration5 is deleted, only 3 WIs
	// should be moved to iteration4
	rest.T().Run("success - verify that workitems are updated correctly", func(t *testing.T) {
		fxt := tf.NewTestFixture(t, rest.DB,
			tf.Iterations(6,
				func(fxt *tf.TestFixture, idx int) error {
					i := fxt.Iterations[idx]
					switch idx {
					case 1:
						i.MakeChildOf(*fxt.Iterations[0])
					case 2:
						i.MakeChildOf(*fxt.Iterations[1])
					case 3:
						i.MakeChildOf(*fxt.Iterations[2])
					case 4:
						i.MakeChildOf(*fxt.Iterations[0])
					case 5:
						i.MakeChildOf(*fxt.Iterations[4])
					}
					return nil
				}),
			tf.WorkItems(20, func(fxt *tf.TestFixture, idx int) error {
				wi := fxt.WorkItems[idx]
				switch idx {
				case 0, 1, 2, 3, 4:
					wi.Fields[workitem.SystemIteration] = fxt.Iterations[1].ID.String()
				case 5, 6, 7, 8, 9:
					wi.Fields[workitem.SystemIteration] = fxt.Iterations[2].ID.String()
				case 10, 11, 12, 13, 14:
					wi.Fields[workitem.SystemIteration] = fxt.Iterations[3].ID.String()
				case 15, 16:
					wi.Fields[workitem.SystemIteration] = fxt.Iterations[4].ID.String()
				case 17, 18, 19:
					wi.Fields[workitem.SystemIteration] = fxt.Iterations[5].ID.String()
				}
				return nil
			}))
		svc, ctrl := rest.SecuredControllerWithIdentity(fxt.Identities[0])
		iterationToDelete := fxt.Iterations[1]
		test.DeleteIterationNoContent(t, svc.Context, svc, ctrl, iterationToDelete.ID)
		wis, err := rest.db.WorkItems().LoadByIteration(svc.Context, iterationToDelete.ID)
		require.Nil(t, err)
		assert.Empty(t, wis)

		// Verify that 15 WIs are moved to Root iteration
		wis, err = rest.db.WorkItems().LoadByIteration(svc.Context, fxt.Iterations[0].ID)
		require.Nil(t, err)
		assert.Len(t, wis, 15)

		// verify included objects
		var mustHave = make(map[uuid.UUID]struct{}, 15)
		for i, wi := range fxt.WorkItems {
			if i < 15 {
				mustHave[wi.ID] = struct{}{}
			}
		}
		require.NotEmpty(t, mustHave)
		for _, itr := range wis {
			if _, ok := mustHave[itr.ID]; ok {
				delete(mustHave, itr.ID)
			}
		}
		require.Empty(t, mustHave)

		iterationToDelete = fxt.Iterations[5]
		test.DeleteIterationNoContent(t, svc.Context, svc, ctrl, iterationToDelete.ID)
		wis, err = rest.db.WorkItems().LoadByIteration(svc.Context, iterationToDelete.ID)
		require.Nil(t, err)
		assert.Empty(t, wis)

		// Verify that 3 WIs are moved to parent of deleted iteration
		wis, err = rest.db.WorkItems().LoadByIteration(svc.Context, fxt.Iterations[4].ID)
		require.Nil(t, err)
		assert.Len(t, wis, 2+3)

		// verify included objects
		mustHave = make(map[uuid.UUID]struct{}, 5)
		for i, wi := range fxt.WorkItems {
			if i >= 15 {
				mustHave[wi.ID] = struct{}{}
			}
		}
		require.NotEmpty(t, mustHave)
		for _, itr := range wis {
			if _, ok := mustHave[itr.ID]; ok {
				delete(mustHave, itr.ID)
			}
		}
		require.Empty(t, mustHave)

		// Verify that no more WIs are moved to Root iteration
		wis, err = rest.db.WorkItems().LoadByIteration(svc.Context, fxt.Iterations[0].ID)
		require.Nil(t, err)
		assert.Len(t, wis, 15)

		// verify included objects
		mustHave = make(map[uuid.UUID]struct{}, 15)
		for i, wi := range fxt.WorkItems {
			if i < 15 {
				mustHave[wi.ID] = struct{}{}
			}
		}
		require.NotEmpty(t, mustHave)
		for _, itr := range wis {
			if _, ok := mustHave[itr.ID]; ok {
				delete(mustHave, itr.ID)
			}
		}
		require.Empty(t, mustHave)

		// verify that child iterations are deleted as well
		deletedIterations := []*iteration.Iteration{
			fxt.Iterations[1],
			fxt.Iterations[2],
			fxt.Iterations[3],
			fxt.Iterations[5],
		}
		for _, i := range deletedIterations {
			_, err := rest.db.Iterations().Load(svc.Context, i.ID)
			require.NotNil(t, err)
			require.IsType(t, errors.NotFoundError{}, err, "error was %v", err)
		}
	})
}
