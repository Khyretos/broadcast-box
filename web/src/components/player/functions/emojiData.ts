// A compact set of commonly used emojis for the chat picker. Each line is an
// emoji followed by search keywords. Any other emoji can still be typed with
// the operating system's emoji keyboard.

export interface EmojiCategory {
	name: string;
	icon: string;
	emojis: { emoji: string; keywords: string }[];
}

const categories: [string, string, string][] = [
	["Smileys", "😀", `
😀 grinning smile happy
😃 smiley happy joy
😄 smile happy laugh
😁 grin beaming
😆 laughing satisfied
😅 sweat smile relief
🤣 rofl rolling floor laughing
😂 joy tears laughing lol
🙂 slight smile
🙃 upside down silly
😉 wink
😊 blush smile
😇 innocent halo angel
🥰 love hearts adore
😍 heart eyes love
🤩 star struck wow
😘 kiss blow
😗 kissing
😚 kissing closed eyes
😋 yum delicious
😛 tongue
😜 wink tongue crazy
🤪 zany crazy goofy
😝 squint tongue
🤑 money mouth rich
🤗 hug hugging
🤭 hand over mouth oops giggle
🤫 shush quiet secret
🤔 thinking hmm
🤐 zipper mouth
🤨 raised eyebrow suspicious sus
😐 neutral
😑 expressionless
😶 no mouth speechless
😏 smirk
😒 unamused
🙄 eye roll
😬 grimace awkward
🤥 lying pinocchio
😌 relieved
😔 pensive sad
😪 sleepy
🤤 drooling
😴 sleeping zzz
😷 mask sick
🤒 thermometer sick
🤕 bandage hurt
🤢 nauseated sick gross
🤮 vomit puke
🥵 hot sweating
🥶 cold freezing
🥴 woozy dizzy drunk
😵 dizzy knocked out
🤯 mind blown exploding head
🤠 cowboy
🥳 party celebrate birthday
😎 cool sunglasses
🤓 nerd glasses
🧐 monocle curious
😕 confused
😟 worried
🙁 frown
☹️ frowning sad
😮 open mouth wow surprised
😯 hushed
😲 astonished shocked
😳 flushed embarrassed
🥺 pleading puppy eyes
😦 frowning open mouth
😧 anguished
😨 fearful scared
😰 anxious sweat
😥 sad relieved
😢 cry sad tear
😭 sob crying loud
😱 scream fear
😖 confounded
😣 persevere
😞 disappointed
😓 downcast sweat
😩 weary tired
😫 tired
🥱 yawn bored
😤 triumph huff
😡 pouting angry rage
😠 angry mad
🤬 cursing swearing
😈 smiling devil
👿 angry devil imp
💀 skull dead
☠️ skull crossbones
💩 poop
🤡 clown
👹 ogre
👺 goblin
👻 ghost boo
👽 alien
👾 space invader game
🤖 robot bot
😺 cat smile
😸 cat grin
😹 cat joy tears
😻 cat heart eyes
😼 cat smirk
🙀 cat weary
😿 cat crying
😾 cat pouting`],
	["Gestures", "👍", `
👋 wave hello hi bye
🤚 raised back of hand
🖐️ hand fingers splayed
✋ raised hand high five
🖖 vulcan salute
👌 ok perfect
🤌 pinched fingers italian
🤏 pinching small
✌️ victory peace
🤞 crossed fingers luck
🤟 love you
🤘 rock on horns metal
🤙 call me shaka
👈 point left
👉 point right
👆 point up
👇 point down
☝️ index up
👍 thumbs up like yes
👎 thumbs down dislike no
✊ fist raised
👊 fist bump punch
🤛 left fist bump
🤜 right fist bump
👏 clap applause
🙌 raising hands celebrate hooray
👐 open hands
🤲 palms up
🤝 handshake deal
🙏 pray please thanks
✍️ writing
💪 muscle strong flex
🦾 mechanical arm
🧠 brain smart
👀 eyes looking
👁️ eye
👄 mouth lips
🫡 salute
🫶 heart hands`],
	["Hearts", "❤️", `
❤️ red heart love
🧡 orange heart
💛 yellow heart
💚 green heart
💙 blue heart
💜 purple heart
🖤 black heart
🤍 white heart
🤎 brown heart
💔 broken heart
❣️ heart exclamation
💕 two hearts
💞 revolving hearts
💓 beating heart
💗 growing heart
💖 sparkling heart
💘 heart arrow cupid
💝 heart ribbon gift
💯 hundred 100 perfect
💢 anger
💥 collision boom
💫 dizzy stars
💦 sweat droplets
💨 dash fast
🕳️ hole
💬 speech bubble chat
💭 thought bubble
💤 zzz sleep`],
	["Animals", "🐶", `
🐶 dog puppy
🐱 cat kitten
🐭 mouse
🐹 hamster
🐰 rabbit bunny
🦊 fox
🐻 bear
🐼 panda
🐨 koala
🐯 tiger
🦁 lion
🐮 cow
🐷 pig
🐸 frog pepe
🐵 monkey
🙈 see no evil monkey
🙉 hear no evil monkey
🙊 speak no evil monkey
🐔 chicken
🐧 penguin
🐦 bird
🐤 chick
🦆 duck
🦅 eagle
🦉 owl
🦇 bat
🐺 wolf
🐗 boar
🐴 horse
🦄 unicorn
🐝 bee
🐛 bug
🦋 butterfly
🐌 snail
🐞 ladybug
🐢 turtle
🐍 snake
🦖 t-rex dinosaur
🐙 octopus
🦑 squid
🦀 crab
🐡 blowfish
🐠 tropical fish
🐟 fish
🐬 dolphin
🐳 whale
🦈 shark
🐊 crocodile
🐘 elephant
🦒 giraffe
🐐 goat goated
🌵 cactus
🌲 tree
🍀 four leaf clover luck
🍁 maple leaf
🌸 cherry blossom
🌹 rose
🌻 sunflower
🌈 rainbow
☀️ sun
🌙 moon
⭐ star
🌟 glowing star
✨ sparkles
⚡ lightning zap
🔥 fire lit hot
❄️ snowflake
🌊 wave ocean`],
	["Food", "🍕", `
🍎 apple
🍌 banana
🍉 watermelon
🍇 grapes
🍓 strawberry
🍒 cherries
🍑 peach
🥑 avocado
🍆 eggplant
🌶️ hot pepper spicy
🌽 corn
🥕 carrot
🥔 potato
🍞 bread
🧀 cheese
🥓 bacon
🍗 chicken leg
🍖 meat
🌭 hot dog
🍔 burger hamburger
🍟 fries
🍕 pizza
🌮 taco
🌯 burrito
🥪 sandwich
🍝 spaghetti pasta
🍜 ramen noodles
🍣 sushi
🍤 shrimp tempura
🍦 ice cream
🍩 donut
🍪 cookie
🎂 birthday cake
🍰 cake
🧁 cupcake
🍫 chocolate
🍬 candy
🍭 lollipop
🍿 popcorn
☕ coffee
🍵 tea
🧋 bubble tea boba
🥤 soda cup
🍺 beer
🍻 cheers beers
🥂 champagne toast
🍷 wine
🥃 whiskey
🍸 cocktail
🧂 salt salty`],
	["Activities", "🎮", `
🎮 video game controller gaming
🕹️ joystick arcade
👾 alien monster game
🎲 dice
♟️ chess
🎯 bullseye target
🎳 bowling
⚽ soccer football
🏀 basketball
🏈 american football
⚾ baseball
🎾 tennis
🏐 volleyball
🏓 ping pong
🥊 boxing glove
🥋 martial arts
⛳ golf
🏆 trophy win champion
🥇 gold medal first
🥈 silver medal second
🥉 bronze medal third
🏅 medal
🎉 party popper tada celebrate
🎊 confetti
🎈 balloon
🎁 gift present
🎤 microphone sing karaoke
🎧 headphones music
🎵 music note
🎶 notes music
🎸 guitar
🥁 drum
🎹 piano keyboard
🎬 clapper movie film
📺 tv television
📷 camera
🎥 movie camera
🎨 art palette paint
🎭 theater
🚀 rocket launch
✈️ airplane
🚗 car
🏎️ race car
🚨 siren alert police`],
	["Objects", "💡", `
💻 laptop computer
🖥️ desktop computer pc
⌨️ keyboard
🖱️ mouse computer
📱 phone mobile
☎️ telephone
🔋 battery
🔌 plug
💡 bulb idea
🔦 flashlight
📚 books
📖 book read
📝 memo note
✏️ pencil
📌 pin
📎 paperclip
✂️ scissors clip cut
🔒 lock
🔑 key
🔨 hammer
🪓 axe
⚔️ swords fight
🛡️ shield
🔫 water pistol
💣 bomb
🧨 firecracker
💰 money bag
💵 dollar money
💎 gem diamond
👑 crown king queen
💍 ring
🕶️ sunglasses
🧢 cap hat
🎩 top hat
⏰ alarm clock
⌛ hourglass time
📅 calendar
📈 chart increasing stonks
📉 chart decreasing
🗑️ trash
🧻 toilet paper
🛒 shopping cart`],
	["Symbols", "✅", `
✅ check mark yes done
☑️ checkbox
✔️ check
❌ cross no wrong
❎ cross mark
➕ plus
➖ minus
❗ exclamation
❓ question
‼️ double exclamation
⁉️ interrobang
⚠️ warning
🚫 prohibited no
⛔ no entry
🔴 red circle live
🟠 orange circle
🟡 yellow circle
🟢 green circle
🔵 blue circle
🟣 purple circle
⚫ black circle
⚪ white circle
🔺 red triangle up
🔻 red triangle down
➡️ right arrow
⬅️ left arrow
⬆️ up arrow
⬇️ down arrow
🔄 repeat
🔁 loop
▶️ play
⏸️ pause
⏹️ stop
⏺️ record
⏭️ next
🔊 speaker loud
🔇 mute
📢 loudspeaker announcement
🔔 bell notification
🆗 ok button
🆕 new
🆒 cool button
🆓 free
🔝 top
💤 sleep
♻️ recycle
🏳️‍🌈 rainbow flag pride
🏴‍☠️ pirate flag`],
];

export const EMOJI_CATEGORIES: EmojiCategory[] = categories.map(([name, icon, list]) => ({
	name,
	icon,
	emojis: list.trim().split("\n").map((line) => {
		const separator = line.indexOf(" ");
		return separator < 0
			? { emoji: line, keywords: "" }
			: { emoji: line.slice(0, separator), keywords: line.slice(separator + 1).toLowerCase() };
	}),
}));
