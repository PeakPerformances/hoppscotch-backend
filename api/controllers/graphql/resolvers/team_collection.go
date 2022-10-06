package resolvers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"

	graphql_context "github.com/jerbob92/hoppscotch-backend/api/controllers/graphql/context"
	"github.com/jerbob92/hoppscotch-backend/models"

	"github.com/graph-gophers/graphql-go"
	"gorm.io/gorm"
)

type TeamCollectionResolver struct {
	c               *graphql_context.Context
	team_collection *models.TeamCollection
}

func NewTeamCollectionResolver(c *graphql_context.Context, team_collection *models.TeamCollection) (*TeamCollectionResolver, error) {
	if team_collection == nil {
		return nil, nil
	}

	return &TeamCollectionResolver{c: c, team_collection: team_collection}, nil
}

func (r *TeamCollectionResolver) ID() (graphql.ID, error) {
	id := graphql.ID(strconv.Itoa(int(r.team_collection.ID)))
	return id, nil
}

func (r *TeamCollectionResolver) Parent() (*TeamCollectionResolver, error) {
	if r.team_collection.ParentID == 0 {
		return nil, nil
	}

	db := r.c.GetDB()
	teamCollection := &models.TeamCollection{}
	err := db.Where("id = ?", r.team_collection.ParentID).First(teamCollection).Error
	if err != nil && err == gorm.ErrRecordNotFound {
		return nil, errors.New("team collection not found")
	}

	if err != nil {
		log.Println(err)
		return nil, err
	}

	return NewTeamCollectionResolver(r.c, teamCollection)
}

func (r *TeamCollectionResolver) Team() (*TeamResolver, error) {
	db := r.c.GetDB()
	team := &models.Team{}
	err := db.Where("id = ?", r.team_collection.TeamID).First(team).Error
	if err != nil && err == gorm.ErrRecordNotFound {
		return nil, errors.New("team collection not found")
	}

	return NewTeamResolver(r.c, team)
}

type TeamCollectionChildrenArgs struct {
	Cursor *string
}

func (r *TeamCollectionResolver) Children(args *TeamCollectionChildrenArgs) ([]*TeamCollectionResolver, error) {
	db := r.c.GetDB()
	teamCollections := []*models.TeamCollection{}
	query := db.Model(&models.TeamCollection{}).Where("parent_id = ?", r.team_collection.ID)
	if args.Cursor != nil && *args.Cursor != "" {
		query.Where("id > ?", args.Cursor)
	}
	err := query.Preload("Team").Find(&teamCollections).Error
	if err != nil {
		return nil, err
	}

	teamCollectionResolvers := []*TeamCollectionResolver{}
	for i := range teamCollections {
		newResolver, err := NewTeamCollectionResolver(r.c, teamCollections[i])
		if err != nil {
			return nil, err
		}
		teamCollectionResolvers = append(teamCollectionResolvers, newResolver)
	}

	return teamCollectionResolvers, nil
}

func (r *TeamCollectionResolver) Title() (string, error) {
	return r.team_collection.Title, nil
}

type CollectionArgs struct {
	CollectionID graphql.ID
}

func (b *BaseQuery) Collection(ctx context.Context, args *CollectionArgs) (*TeamCollectionResolver, error) {
	c := b.GetReqC(ctx)
	db := c.GetDB()
	collection := &models.TeamCollection{}
	err := db.Model(&models.TeamCollection{}).Where("id = ?", args.CollectionID).First(collection).Error
	if err != nil && err == gorm.ErrRecordNotFound {
		return nil, errors.New("you do not have access to this collection")
	}
	if err != nil {
		return nil, err
	}

	userRole, err := getUserRoleInTeam(ctx, c, collection.TeamID)
	if err != nil {
		return nil, err
	}

	if userRole == nil {
		return nil, errors.New("you do not have access to this collection")
	}

	return NewTeamCollectionResolver(c, collection)
}

type CollectionsOfTeamArgs struct {
	Cursor *graphql.ID
	TeamID graphql.ID
}

func (b *BaseQuery) CollectionsOfTeam(ctx context.Context, args *CollectionsOfTeamArgs) ([]*TeamCollectionResolver, error) {
	c := b.GetReqC(ctx)
	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("user not in team")
	}

	db := c.GetDB()
	teamCollections := []*models.TeamCollection{}
	query := db.Model(&models.TeamCollection{}).Where("team_id = ?", args.TeamID)
	if args.Cursor != nil && *args.Cursor != "" {
		query.Where("id > ?", args.Cursor)
	}
	err = query.Find(&teamCollections).Error
	if err != nil {
		return nil, err
	}

	teamCollectionResolvers := []*TeamCollectionResolver{}
	for i := range teamCollections {
		newResolver, err := NewTeamCollectionResolver(c, teamCollections[i])
		if err != nil {
			return nil, err
		}
		teamCollectionResolvers = append(teamCollectionResolvers, newResolver)
	}

	return teamCollectionResolvers, nil
}

type ExportCollectionsToJSONArgs struct {
	TeamID graphql.ID
}

type ExportJSONCollectionRequest map[string]interface{}

type ExportJSONCollection struct {
	Version  int                           `json:"v"`
	Name     string                        `json:"name"`
	Folders  []ExportJSONCollection        `json:"folders"`
	Requests []ExportJSONCollectionRequest `json:"requests"`
}

func GetTeamExportJSON(c *graphql_context.Context, teamID graphql.ID, parentID uint) ([]ExportJSONCollection, error) {
	db := c.GetDB()
	collections := []*models.TeamCollection{}
	err := db.Model(&models.TeamCollection{}).Where("team_id = ? AND parent_id = ?", teamID, parentID).Find(&collections).Error
	if err != nil {
		return nil, err
	}

	output := []ExportJSONCollection{}
	for i := range collections {
		collection := ExportJSONCollection{
			Version:  1,
			Name:     collections[i].Title,
			Folders:  []ExportJSONCollection{},
			Requests: []ExportJSONCollectionRequest{},
		}

		requests := []*models.TeamRequest{}
		err := db.Model(&models.TeamRequest{}).Where("team_id = ? AND team_collection_id = ?", teamID, collections[i].ID).Find(&requests).Error
		if err != nil {
			return nil, err
		}

		for ri := range requests {
			requestDecode := ExportJSONCollectionRequest{}
			json.Unmarshal([]byte(requests[ri].Request), &requestDecode)

			collection.Requests = append(collection.Requests, requestDecode)
		}

		subfolders, err := GetTeamExportJSON(c, teamID, collections[i].ID)
		if err != nil {
			return nil, err
		}

		collection.Folders = subfolders

		output = append(output, collection)
	}
	return output, nil
}

func (b *BaseQuery) ExportCollectionsToJSON(ctx context.Context, args *ExportCollectionsToJSONArgs) (string, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return "", err
	}

	if userRole == nil {
		return "", errors.New("you do not have access to this team")
	}

	teamExport, err := GetTeamExportJSON(c, args.TeamID, 0)
	if err != nil {
		return "", err
	}

	exportJSON, err := json.MarshalIndent(teamExport, "", "  ")
	if err != nil {
		return "", err
	}

	return string(exportJSON), nil
}

type RequestsInCollectionArgs struct {
	CollectionID graphql.ID
	Cursor       *graphql.ID
}

func (b *BaseQuery) RequestsInCollection(ctx context.Context, args *RequestsInCollectionArgs) ([]*TeamRequestResolver, error) {
	c := b.GetReqC(ctx)
	db := c.GetDB()
	collection := &models.TeamCollection{}
	err := db.Model(&models.TeamCollection{}).Where("id = ?", args.CollectionID).First(collection).Error
	if err != nil && err == gorm.ErrRecordNotFound {
		return nil, errors.New("you do not have access to this collection")
	}
	if err != nil {
		return nil, err
	}

	userRole, err := getUserRoleInTeam(ctx, c, collection.TeamID)
	if err != nil {
		return nil, err
	}

	if userRole == nil {
		return nil, errors.New("you do not have access to this collection")
	}

	teamRequests := []*models.TeamRequest{}
	query := db.Model(&models.TeamRequest{}).Where("team_collection_id", args.CollectionID)
	if args.Cursor != nil && *args.Cursor != "" {
		query.Where("id > ?", args.Cursor)
	}
	err = query.Find(&teamRequests).Error
	if err != nil {
		return nil, err
	}

	teamRequestResolvers := []*TeamRequestResolver{}
	for i := range teamRequests {
		newResolver, err := NewTeamRequestResolver(c, teamRequests[i])
		if err != nil {
			return nil, err
		}
		teamRequestResolvers = append(teamRequestResolvers, newResolver)
	}

	return teamRequestResolvers, nil
}

type CreateChildCollectionArgs struct {
	ChildTitle   string
	CollectionID graphql.ID
}

func (b *BaseQuery) CreateChildCollection(ctx context.Context, args *CreateChildCollectionArgs) (*TeamCollectionResolver, error) {
	c := b.GetReqC(ctx)
	db := c.GetDB()
	collection := &models.TeamCollection{}
	err := db.Model(&models.TeamCollection{}).Where("id = ?", args.CollectionID).First(collection).Error
	if err != nil && err == gorm.ErrRecordNotFound {
		return nil, errors.New("you do not have access to this collection")
	}
	if err != nil {
		return nil, err
	}

	userRole, err := getUserRoleInTeam(ctx, c, collection.TeamID)
	if err != nil {
		return nil, err
	}

	if userRole == nil {
		return nil, errors.New("you do not have access to this collection")
	}

	if *userRole == models.Owner || *userRole == models.Editor {
		newCollection := &models.TeamCollection{
			Title:    args.ChildTitle,
			ParentID: collection.ID,
			TeamID:   collection.TeamID,
		}
		err := db.Save(newCollection).Error
		if err != nil {
			return nil, err
		}

		resolver, err := NewTeamCollectionResolver(c, newCollection)
		if err != nil {
			return nil, err
		}

		bus.Publish("team:"+strconv.Itoa(int(newCollection.TeamID))+":collections:added", resolver)

		return resolver, nil
	}

	return nil, errors.New("you are not allowed to create a collection in this team")
}

type CreateTeamRequestInput struct {
	Request string
	TeamID  graphql.ID
	Title   string
}

type CreateRequestInCollectionArgs struct {
	CollectionID graphql.ID
	Data         CreateTeamRequestInput
}

func (b *BaseQuery) CreateRequestInCollection(ctx context.Context, args *CreateRequestInCollectionArgs) (*TeamRequestResolver, error) {
	c := b.GetReqC(ctx)
	db := c.GetDB()
	collection := &models.TeamCollection{}
	err := db.Model(&models.TeamCollection{}).Where("id = ?", args.CollectionID).First(collection).Error
	if err != nil && err == gorm.ErrRecordNotFound {
		return nil, errors.New("you do not have access to this collection")
	}
	if err != nil {
		return nil, err
	}

	userRole, err := getUserRoleInTeam(ctx, c, collection.TeamID)
	if err != nil {
		return nil, err
	}

	if userRole == nil {
		return nil, errors.New("you do not have access to this collection")
	}

	if *userRole == models.Owner || *userRole == models.Editor {
		newRequest := &models.TeamRequest{
			TeamCollectionID: collection.ID,
			TeamID:           collection.TeamID,
			Title:            args.Data.Title,
			Request:          args.Data.Request,
		}
		err := db.Save(newRequest).Error
		if err != nil {
			return nil, err
		}

		resolver, err := NewTeamRequestResolver(c, newRequest)
		if err != nil {
			return nil, err
		}

		bus.Publish("team:"+strconv.Itoa(int(newRequest.TeamID))+":requests:added", resolver)

		return resolver, nil
	}

	return nil, errors.New("you are not allowed to create a request in this team")
}

type CreateRootCollectionArgs struct {
	TeamID graphql.ID
	Title  string
}

func (b *BaseQuery) CreateRootCollection(ctx context.Context, args *CreateRootCollectionArgs) (*TeamCollectionResolver, error) {
	c := b.GetReqC(ctx)
	db := c.GetDB()

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}

	if userRole == nil {
		return nil, errors.New("you do not have access to this collection")
	}

	if *userRole == models.Owner || *userRole == models.Editor {
		parsedTeamID, _ := strconv.Atoi(string(args.TeamID))
		newCollection := &models.TeamCollection{
			Title:  args.Title,
			TeamID: uint(parsedTeamID),
		}
		err := db.Save(newCollection).Error
		if err != nil {
			return nil, err
		}

		resolver, err := NewTeamCollectionResolver(c, newCollection)
		if err != nil {
			return nil, err
		}

		bus.Publish("team:"+strconv.Itoa(int(newCollection.TeamID))+":collections:added", resolver)

		return resolver, nil
	}

	return nil, errors.New("you are not allowed to create a collection in this team")
}

type DeleteCollectionArgs struct {
	CollectionID graphql.ID
}

func (b *BaseQuery) DeleteCollection(ctx context.Context, args *DeleteCollectionArgs) (bool, error) {
	c := b.GetReqC(ctx)
	db := c.GetDB()
	collection := &models.TeamCollection{}
	err := db.Model(&models.TeamCollection{}).Where("id = ?", args.CollectionID).First(collection).Error
	if err != nil && err == gorm.ErrRecordNotFound {
		return false, errors.New("you do not have access to this collection")
	}
	if err != nil {
		return false, err
	}

	userRole, err := getUserRoleInTeam(ctx, c, collection.TeamID)
	if err != nil {
		return false, err
	}

	if userRole == nil {
		return false, errors.New("you do not have access to this collection")
	}

	if *userRole == models.Owner || *userRole == models.Editor {
		err := db.Delete(collection).Error
		if err != nil {
			return false, err
		}

		bus.Publish("team:"+strconv.Itoa(int(collection.TeamID))+":collections:removed", graphql.ID(strconv.Itoa(int(collection.ID))))

		return true, nil
	}

	return false, errors.New("you are not allowed to delete a collection in this team")
}

type ImportCollectionFromUserFirestoreArgs struct {
	FBCollectionPath   string
	ParentCollectionID *graphql.ID
	TeamID             graphql.ID
}

func (b *BaseQuery) ImportCollectionFromUserFirestore(ctx context.Context, args *ImportCollectionFromUserFirestoreArgs) (*TeamCollectionResolver, error) {
	// This doesn't seem to be used (anymore).
	return nil, nil
}

type ImportCollectionsFromJSONArgs struct {
	JSONString         string
	ParentCollectionID *graphql.ID
	TeamID             graphql.ID
}

func importJSON(c *graphql_context.Context, teamID uint, parentID uint, folders []ExportJSONCollection) error {
	db := c.GetDB()
	for i := range folders {
		newCollection := &models.TeamCollection{
			TeamID:   teamID,
			Title:    folders[i].Name,
			ParentID: parentID,
		}

		err := db.Save(newCollection).Error
		if err != nil {
			return err
		}

		resolver, err := NewTeamCollectionResolver(c, newCollection)
		if err != nil {
			return err
		}

		bus.Publish("team:"+strconv.Itoa(int(teamID))+":collections:added", resolver)

		if folders[i].Requests != nil && len(folders[i].Requests) > 0 {
			for ri := range folders[i].Requests {
				newTeamRequest := &models.TeamRequest{
					TeamID:           teamID,
					TeamCollectionID: newCollection.ID,
				}

				if nameVal, ok := folders[i].Requests[ri]["name"]; ok {
					name, ok := nameVal.(string)
					if ok {
						newTeamRequest.Title = name
					}
				}

				requestData, err := json.Marshal(folders[i].Requests[ri])
				if err != nil {
					return err
				}

				newTeamRequest.Request = string(requestData)

				err = db.Save(newTeamRequest).Error
				if err != nil {
					return err
				}

				requestResolver, err := NewTeamRequestResolver(c, newTeamRequest)
				if err != nil {
					return err
				}

				bus.Publish("team:"+strconv.Itoa(int(teamID))+":requests:added", requestResolver)
			}
		}

		if folders[i].Folders != nil && len(folders[i].Folders) > 0 {
			err = importJSON(c, teamID, newCollection.ID, folders[i].Folders)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (b *BaseQuery) ImportCollectionsFromJSON(ctx context.Context, args *ImportCollectionsFromJSONArgs) (bool, error) {
	c := b.GetReqC(ctx)
	db := c.GetDB()

	parentCollectionID := uint(0)
	if args.ParentCollectionID != nil {
		collection := &models.TeamCollection{}
		err := db.Model(&models.TeamCollection{}).Where("id = ?", args.ParentCollectionID).First(collection).Error
		if err != nil && err == gorm.ErrRecordNotFound {
			return false, errors.New("you do not have access to this collection")
		}
		if err != nil {
			return false, err
		}

		userRole, err := getUserRoleInTeam(ctx, c, collection.TeamID)
		if err != nil {
			return false, err
		}

		if userRole == nil {
			return false, errors.New("you do not have access to this collection")
		}

		if *userRole == models.Owner || *userRole == models.Editor {
			parentCollectionID = collection.ID
		} else {
			return false, errors.New("you do not have write access to this collection")
		}
	}

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return false, err
	}

	if userRole == nil {
		return false, errors.New("you do not have access to this collection")
	}

	if *userRole == models.Owner || *userRole == models.Editor {
		importData := []ExportJSONCollection{}
		err := json.Unmarshal([]byte(args.JSONString), &importData)
		if err != nil {
			return false, err
		}

		teamID, _ := strconv.Atoi(string(args.TeamID))
		err = importJSON(c, uint(teamID), parentCollectionID, importData)
		if err != nil {
			return false, err
		}

		return true, nil
	}

	return false, errors.New("you do not have write access to this team")
}

type RenameCollectionArgs struct {
	CollectionID graphql.ID
	NewTitle     string
}

func (b *BaseQuery) RenameCollection(ctx context.Context, args *RenameCollectionArgs) (*TeamCollectionResolver, error) {
	c := b.GetReqC(ctx)
	db := c.GetDB()
	collection := &models.TeamCollection{}
	err := db.Model(&models.TeamCollection{}).Where("id = ?", args.CollectionID).First(collection).Error
	if err != nil && err == gorm.ErrRecordNotFound {
		return nil, errors.New("you do not have access to this collection")
	}
	if err != nil {
		return nil, err
	}

	userRole, err := getUserRoleInTeam(ctx, c, collection.TeamID)
	if err != nil {
		return nil, err
	}

	if userRole == nil {
		return nil, errors.New("you do not have access to this collection")
	}

	if *userRole == models.Owner || *userRole == models.Editor {
		collection.Title = args.NewTitle
		err := db.Save(collection).Error
		if err != nil {
			return nil, err
		}

		resolver, err := NewTeamCollectionResolver(c, collection)
		if err != nil {
			return nil, err
		}

		bus.Publish("team:"+strconv.Itoa(int(collection.TeamID))+":collections:updated", resolver)

		return NewTeamCollectionResolver(c, collection)
	}

	return nil, errors.New("you are not allowed to rename a collection in this team")
}

type ReplaceCollectionsWithJSONArgs struct {
	JSONString         string
	ParentCollectionID *graphql.ID
	TeamID             graphql.ID
}

func (b *BaseQuery) ReplaceCollectionsWithJSON(ctx context.Context, args *ReplaceCollectionsWithJSONArgs) (bool, error) {
	// This doesn't seem to be used (anymore).
	return false, nil
}

type SubscriptionArgs struct {
	TeamID graphql.ID
}

func (b *BaseQuery) TeamCollectionAdded(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamCollectionResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamCollectionResolver)
	eventHandler := func(resolver *TeamCollectionResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":collections:added", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamCollectionRemoved(ctx context.Context, args *SubscriptionArgs) (<-chan graphql.ID, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan graphql.ID)
	eventHandler := func(resolver graphql.ID) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":collections:removed", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamCollectionUpdated(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamCollectionResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamCollectionResolver)
	eventHandler := func(resolver *TeamCollectionResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":collections:updated", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamInvitationAdded(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamInvitationResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamInvitationResolver)
	eventHandler := func(resolver *TeamInvitationResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":invitations:added", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamInvitationRemoved(ctx context.Context, args *SubscriptionArgs) (<-chan graphql.ID, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan graphql.ID)
	eventHandler := func(resolver graphql.ID) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":invitations:removed", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamMemberAdded(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamMemberResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamMemberResolver)
	eventHandler := func(resolver *TeamMemberResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":members:added", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamMemberRemoved(ctx context.Context, args *SubscriptionArgs) (<-chan graphql.ID, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan graphql.ID)
	eventHandler := func(resolver graphql.ID) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":members:removed", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamMemberUpdated(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamMemberResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamMemberResolver)
	eventHandler := func(resolver *TeamMemberResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":members:updated", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamRequestAdded(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamRequestResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamRequestResolver)
	eventHandler := func(resolver *TeamRequestResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":requests:added", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamRequestDeleted(ctx context.Context, args *SubscriptionArgs) (<-chan graphql.ID, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan graphql.ID)
	eventHandler := func(resolver graphql.ID) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":requests:deleted", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamRequestUpdated(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamRequestResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamRequestResolver)
	eventHandler := func(resolver *TeamRequestResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":requests:updated", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamEnvironmentCreated(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamEnvironmentResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamEnvironmentResolver)
	eventHandler := func(resolver *TeamEnvironmentResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":environments:created", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamEnvironmentDeleted(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamEnvironmentResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamEnvironmentResolver)
	eventHandler := func(resolver *TeamEnvironmentResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":environments:deleted", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}

func (b *BaseQuery) TeamEnvironmentUpdated(ctx context.Context, args *SubscriptionArgs) (<-chan *TeamEnvironmentResolver, error) {
	c := b.GetReqC(ctx)

	userRole, err := getUserRoleInTeam(ctx, c, args.TeamID)
	if err != nil {
		return nil, err
	}
	if userRole == nil {
		return nil, errors.New("no access to team")
	}

	teamID, _ := strconv.Atoi(string(args.TeamID))
	notificationChannel := make(chan *TeamEnvironmentResolver)
	eventHandler := func(resolver *TeamEnvironmentResolver) {
		notificationChannel <- resolver
	}

	err = subscribeUntilDone(ctx, "team:"+strconv.Itoa(teamID)+":environments:updated", eventHandler)
	if err != nil {
		return nil, err
	}

	return notificationChannel, nil
}
