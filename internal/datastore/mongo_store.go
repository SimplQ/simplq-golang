package datastore

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"

	"github.com/SimplQ/simplQ-golang/internal/models/db"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// A Structure with Collections frequently used and a pointer to the client
type MongoDB struct {
	Client *mongo.Client
	Queue  *mongo.Collection
	Token  *mongo.Collection
}

// Mutex to make sure only one add token request is running at a time
var queueMutex sync.Mutex

func NewMongoDB() *MongoDB {
	// Use local development mongodb instance if env variable not set
	uri := "mongodb://root:example@localhost:27017/?maxPoolSize=20&w=majority"

	if val, ok := os.LookupEnv("MONGO_URI"); ok {
		uri = val
	}

	log.Println("Connecting to MongoDB...")

	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(uri))

	if err != nil {
		log.Fatal(err)
	}

	new_mongodb := MongoDB{
		client,
		client.Database("simplq").Collection("queue"),
		client.Database("simplq").Collection("token"),
	}

	log.Println("Successfully connected to MongoDB!")

	return &new_mongodb
}

func (mongodb MongoDB) CreateQueue(queue db.Queue) (db.QueueId, error) {
	// Set id to empty so its generated by mongoDB
	queue.Id = ""

	result, err := mongodb.Queue.InsertOne(context.TODO(), queue)

	if err != nil {
		return queue.Id, err
	}

	stringId := result.InsertedID.(primitive.ObjectID).Hex()

	return db.QueueId(stringId), nil
}

func (mongodb MongoDB) ReadQueue(id db.QueueId) (db.Queue, error) {
	result := db.Queue{}

	// convert id string to ObjectId
	queueId, _ := primitive.ObjectIDFromHex(string(id))

	err := mongodb.Queue.FindOne(context.TODO(), bson.M{"_id": queueId}).Decode(&result)

	if err != nil {
		return result, err
	}

	filter := bson.D{
		// queueid is a string this time
		{Key: "queueid", Value: id},
	}

	// sort tokens by ascending order of token number
	sort := bson.D{
		{Key: "tokennumber", Value: 1},
	}

	findOptions := options.Find()
	findOptions.SetSort(sort)

	cursor, err := mongodb.Token.Find(context.TODO(), filter, findOptions)

	var tokens []db.Token

	if err = cursor.All(context.TODO(), &tokens); err != nil {
		log.Fatal(err)
		return result, err
	}

	log.Printf("%d tokens in queue", len(tokens))

	result.Tokens = tokens

	return result, nil
}

func (mongodb MongoDB) SetIsPaused(id db.QueueId, isPaused bool) error {
	queueId, _ := primitive.ObjectIDFromHex(string(id))
	result, err := mongodb.Queue.UpdateOne(
		context.TODO(),
		bson.M{"_id": queueId},
		bson.D{
			{Key: "$set", Value: bson.D{primitive.E{Key: "isPaused", Value: isPaused}}},
		},
	)
	if err != nil {
		return err
	}
	log.Printf("Records updated:%d", result.ModifiedCount)
	if result.ModifiedCount == 0 {
		return errors.New("no record found")
	}
	return nil

}

func (mongodb MongoDB) DeleteQueue(id db.QueueId) error {
	queueId, _ := primitive.ObjectIDFromHex(string(id))
	result, err := mongodb.Queue.DeleteOne(
		context.TODO(),
		bson.M{"_id": queueId})

	if err != nil {
		return err
	}
	log.Printf("Records deleted:%d", result.DeletedCount)
	if result.DeletedCount == 0 {
		return errors.New("no record found")
	}
	return nil
}

func (mongodb MongoDB) AddTokenToQueue(id db.QueueId, token db.Token) (db.TokenId, error) {
	token.Id = ""
	token.QueueId = id

	// Lock queue to ensure 2 concurrent add tokens don't happen
	queueMutex.Lock()

	// if there were 2 concurrent calls to this function, 2 tokens might get the same number
	max, err := mongodb.GetMaxToken(id)

	if err != nil {
		log.Fatal(err)
		queueMutex.Unlock()
		return token.Id, err
	}

	token.TokenNumber = max + 1

	result, err := mongodb.Token.InsertOne(context.TODO(), token)

	queueMutex.Unlock()

	if err != nil {
        log.Println(err);
		return token.Id, err
	}

	stringId := result.InsertedID.(primitive.ObjectID).Hex()

	return db.TokenId(stringId), nil
}

func (mongodb MongoDB) GetMaxToken(id db.QueueId) (uint32, error) {
	filter := bson.D{
		// queueid is a string this time
		{Key: "queueid", Value: id},
	}

	sort := bson.D{
		{Key: "tokennumber", Value: -1},
	}

	findOptions := options.Find()
	findOptions.SetSort(sort)
	findOptions.SetLimit(1)

	cursor, err := mongodb.Token.Find(context.TODO(), filter, findOptions)

	if err != nil {
		return 0, err
	}

	var tokens []db.Token

	if err = cursor.All(context.TODO(), &tokens); err != nil {
		log.Println(err)
		return 0, err
	}

	if len(tokens) <= 0 {
		return 0, nil
	}

	return tokens[0].TokenNumber, nil
}

func (mongodb MongoDB) ReadToken(id db.TokenId) (db.Token, error) {
	tokenId, _ := primitive.ObjectIDFromHex(string(id))

	var result db.Token

	err := mongodb.Token.FindOne(context.TODO(), bson.M{"_id": tokenId}).Decode(&result)

	if err != nil {
		log.Fatal(err)
		return result, err
	}

	return result, nil
}

func (mongodb MongoDB) RemoveToken(db.TokenId) error {
	panic("Not implemented")
}
