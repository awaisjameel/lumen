package search

// Label is a thing the AI looks for: a name users see and the captions
// SigLIP scores images against (the best-scoring caption counts).
//
// SigLIP was trained on web captions, so short caption-like phrases ("a
// group photo") score far better than "a photo of a …" templates, and
// "a screenshot of …" phrases match most screenshots whatever they show.
// Prompts were chosen by measuring them on a real library.
//
// A label applies when SigLIP's probability for it passes the user's
// confidence setting. Generic concepts ("a person") rarely do, because web
// captions are specific; so the broad kinds of photo are Exclusive: every
// photo also gets whichever kind fits it best (when one clearly does),
// which found 94% of the people photos in that library against 16% by
// probability alone.
//
// Screenshots are shown to the model at 224 px, where their text is
// unreadable, so things that live inside screenshots (chats, receipts…)
// are judged on layout alone and need a surer score: a Floor above the
// user's setting. Some subjects could not be told apart at all (code from
// other interfaces) and are left to search, which ranks them well.
type Label struct {
	Name      string
	Prompts   []string
	Exclusive bool    // a broad kind of photo: competes with the other kinds
	Sink      bool    // a kind that only absorbs photos fitting no real kind; never shown
	Floor     float32 // the least probability that counts, whatever the setting
}

// VocabularyVersion changes whenever Vocabulary does, so existing libraries
// pick up new or re-worded labels (and are re-tagged) on the next start.
const VocabularyVersion = 3

// onScreen is the floor for things judged inside screenshots.
const onScreen = 0.05

// thing is an ordinary label, kind an Exclusive one, sink a hidden kind.
func thing(name string, prompts ...string) Label { return Label{Name: name, Prompts: prompts} }

func kind(name string, prompts ...string) Label {
	return Label{Name: name, Prompts: prompts, Exclusive: true}
}

func sink(name string, prompts ...string) Label {
	return Label{Name: name, Prompts: prompts, Exclusive: true, Sink: true}
}

// sure raises a label's floor to onScreen (see Label).
func (l Label) sure() Label {
	l.Floor = onScreen
	return l
}

// Vocabulary is Lumen's built-in list of Things.
var Vocabulary = []Label{
	// The broad kinds of photo.
	kind("People", "a person", "a group photo", "a group of men", "a group of women", "a man", "a woman", "a child"),
	kind("Screenshots", "a screenshot", "a screenshot of a website", "a screenshot of a phone", "a screenshot of source code"),
	kind("Documents", "a document", "a page of text", "a scanned document", "a receipt"),
	kind("Nature", "nature", "a landscape", "the sky", "flowers"),
	kind("Food", "food", "a meal", "a drink"),
	kind("Animals", "an animal", "a pet"),
	kind("Places", "a building", "a city", "a street", "a room", "an interior"),
	kind("Vehicles", "a car", "a vehicle"),
	sink("Other objects", "an object", "a close-up of an object"),
	kind("Illustrations", "a painting", "an illustration", "a cartoon", "a logo"),

	// People and moments
	thing("Group photos", "a group photo", "a group of people posing for a photo"),
	thing("Selfies", "a selfie"),
	thing("Portraits", "a portrait photo", "a headshot"),
	thing("Babies", "a baby", "a newborn baby"),
	thing("Children", "a child", "kids playing"),
	thing("Couples", "a couple", "a couple posing together"),
	thing("Families", "a family photo"),
	thing("Crowds", "a crowd of people"),
	thing("Weddings", "a wedding", "a bride and groom"),
	thing("Birthdays", "a birthday party", "a birthday cake"),
	thing("Parties", "a party", "people celebrating"),
	thing("Concerts", "a concert", "a band playing on stage"),
	thing("Graduations", "a graduation ceremony"),
	thing("Meetings", "people in a meeting", "people in an office"),
	thing("Presentations", "a person giving a presentation", "a conference talk"),
	thing("Classrooms", "a classroom"),
	thing("Sports", "people playing sports", "a sports match"),
	thing("Football", "football", "soccer players"),
	thing("Cricket", "a cricket match", "cricket"),
	thing("Basketball", "basketball"),
	thing("Tennis", "tennis"),
	thing("Running", "people running", "a running race"),
	thing("Cycling", "cycling", "a person riding a bicycle"),
	thing("Swimming", "swimming", "a swimming pool"),
	thing("Gym", "a gym", "a person working out"),
	thing("Yoga", "yoga"),
	thing("Hiking", "hiking", "a hiking trail"),
	thing("Camping", "camping", "a tent"),
	thing("Skiing", "skiing", "snowboarding"),
	thing("Fishing", "fishing"),
	thing("Dancing", "people dancing"),
	thing("Prayer", "people praying"),
	thing("Shopping", "shopping", "a shop"),
	thing("Travel", "a tourist", "travel photo"),
	thing("Costumes", "people in costumes"),
	thing("Fireworks", "fireworks"),
	thing("Christmas", "christmas", "a christmas tree"),
	thing("Halloween", "halloween"),
	thing("Gifts", "a gift", "wrapped presents"),
	thing("Balloons", "balloons"),

	// Animals
	thing("Dogs", "a dog", "a puppy"),
	thing("Cats", "a cat", "a kitten"),
	thing("Birds", "a bird"),
	thing("Horses", "a horse"),
	thing("Cows", "a cow", "cattle"),
	thing("Sheep and goats", "a sheep", "a goat"),
	thing("Chickens", "a chicken"),
	thing("Fish", "a fish", "an aquarium"),
	thing("Insects", "an insect", "a butterfly"),
	thing("Wildlife", "wild animals", "a wild animal"),
	thing("Zoo", "a zoo"),
	thing("Elephants", "an elephant"),
	thing("Monkeys", "a monkey"),

	// Food and drink
	thing("Restaurants", "a restaurant", "people eating at a restaurant"),
	thing("Breakfast", "breakfast"),
	thing("Desserts", "a dessert"),
	thing("Cakes", "a cake"),
	thing("Ice cream", "ice cream"),
	thing("Pizza", "pizza"),
	thing("Burgers", "a burger"),
	thing("Barbecue", "a barbecue", "grilled meat"),
	thing("Biryani and rice", "biryani", "a rice dish"),
	thing("Bread", "bread"),
	thing("Fruit", "fruit"),
	thing("Vegetables", "vegetables"),
	thing("Salad", "a salad"),
	thing("Sushi", "sushi"),
	thing("Noodles", "noodles"),
	thing("Coffee", "a cup of coffee", "coffee"),
	thing("Tea", "a cup of tea", "tea"),
	thing("Drinks", "a drink", "a cocktail"),
	thing("Cooking", "cooking", "a kitchen"),

	// Nature and places
	thing("Beaches", "a beach"),
	thing("Sea", "the sea", "the ocean"),
	thing("Lakes", "a lake"),
	thing("Rivers", "a river"),
	thing("Waterfalls", "a waterfall"),
	thing("Mountains", "mountains"),
	thing("Snow", "snow"),
	thing("Forests", "a forest"),
	thing("Trees", "a tree"),
	thing("Gardens", "a garden"),
	thing("Parks", "a park"),
	thing("Flowers", "a flower", "flowers"),
	thing("Plants", "a plant", "a potted plant"),
	thing("Deserts", "a desert"),
	thing("Fields", "a field", "farmland"),
	thing("Sunsets", "a sunset", "a sunrise"),
	thing("Sky", "the sky", "clouds"),
	thing("Night", "a photo at night", "the night sky"),
	thing("Stars", "stars in the night sky", "the milky way"),
	thing("Rain", "rain"),
	thing("Rainbows", "a rainbow"),
	thing("Cities", "a city", "a city skyline"),
	thing("Streets", "a street"),
	thing("Buildings", "a building"),
	thing("Architecture", "architecture"),
	thing("Houses", "a house"),
	thing("Bridges", "a bridge"),
	thing("Mosques", "a mosque"),
	thing("Churches", "a church"),
	thing("Temples", "a temple"),
	thing("Monuments", "a monument", "a landmark"),
	thing("Castles and forts", "a castle", "a fort"),
	thing("Museums", "a museum"),
	thing("Markets", "a market", "a bazaar"),
	thing("Airports", "an airport"),
	thing("Stations", "a train station"),
	thing("Stadiums", "a stadium"),
	thing("Offices", "an office"),
	thing("Rooms", "a living room", "a bedroom"),
	thing("Hotels", "a hotel room", "a hotel"),
	thing("Pools", "a swimming pool"),
	thing("Playgrounds", "a playground"),
	thing("Aerial views", "an aerial view", "a drone photo"),
	thing("Underwater", "an underwater photo"),

	// Vehicles
	thing("Cars", "a car"),
	thing("Motorcycles", "a motorcycle"),
	thing("Bicycles", "a bicycle"),
	thing("Buses", "a bus"),
	thing("Trucks", "a truck"),
	thing("Trains", "a train"),
	thing("Planes", "an airplane"),
	thing("Boats", "a boat", "a ship"),
	thing("Rickshaws", "a rickshaw", "an auto rickshaw"),
	thing("Traffic", "traffic", "a traffic jam"),

	// Things
	thing("Phones", "a mobile phone", "a smartphone"),
	thing("Computers", "a computer", "a laptop"),
	thing("Books", "a book", "books"),
	thing("Toys", "a toy", "toys"),
	thing("Clothes", "clothes", "a dress"),
	thing("Shoes", "shoes"),
	thing("Jewellery", "jewellery", "a ring"),
	thing("Watches", "a watch"),
	thing("Bags", "a bag", "a handbag"),
	thing("Furniture", "furniture", "a sofa"),
	thing("Paintings", "a painting", "an artwork"),
	thing("Sculptures", "a sculpture", "a statue"),
	thing("Musical instruments", "a musical instrument", "a guitar"),
	thing("Cameras", "a camera"),
	thing("Tools", "tools"),
	thing("Bottles", "a bottle"),
	thing("Candles", "candles"),
	thing("Decorations", "decorations", "fairy lights"),
	thing("Flags", "a flag"),
	thing("Trophies", "a trophy", "a medal"),
	thing("Cards", "a greeting card"),
	thing("Tattoos", "a tattoo"),

	// Documents and screens
	thing("Chats", "a whatsapp chat", "a text message conversation").sure(),
	thing("Receipts", "a receipt", "a till receipt", "a shopping receipt").sure(),
	thing("ID cards", "an id card", "a passport"),
	thing("Tickets", "a ticket", "a boarding pass"),
	thing("Handwriting", "handwriting", "a handwritten note"),
	thing("Whiteboards", "a whiteboard"),
	thing("Charts", "a chart", "a bar chart", "a pie chart", "a graph").sure(),
	thing("Spreadsheets", "a spreadsheet", "an excel spreadsheet").sure(),
	thing("Maps", "a map"),
	thing("Menus", "a restaurant menu"),
	thing("Signs", "a sign", "a signboard"),
	thing("Posters", "a poster", "a flyer"),
	thing("Memes", "a meme"),
	thing("Quotes", "a quote", "an inspirational quote"),
	thing("QR codes", "a qr code", "a barcode"),
	thing("Logos", "a logo"),
	thing("Diagrams", "a diagram", "a flowchart"),
	thing("Screens", "a photo of a computer screen", "a photo of a tv screen"),

	// Photo types
	thing("Black and white", "a black and white photo"),
	thing("Close-ups", "a close-up photo", "a macro photo"),
	thing("Blurry", "a blurry photo"),
	thing("Dark", "a very dark photo"),
	thing("Panoramas", "a panorama"),
	thing("Old photos", "an old photo", "a vintage photograph"),
}
