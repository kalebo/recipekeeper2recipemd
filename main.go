package main

import (
  "fmt"
  "log"
  "io"
  "os"
  "time"
  "strconv"
  "strings"

  "errors"
  "regexp"

  "github.com/PuerkitoBio/goquery"
)

// TODOS:
//  [] - Parse out ammount and unit of the ingredients and wrap in asterisks
//  [] - Consider including images
//  [] - Consider writing out nutrition
//  [] - Extract linked recipes (missing in export data)
//  [] - Decide if we should purge the non ascii characters or not. If so include bullets and degree symbols in the replacement list
//  [] - If we continue replacing the fractions we should ensure that the are spaces before them to avoid improper fractions being rendered as  11/2 rather than 1 1/2
//  [] - Parse instructions to see if they have a trailing colon and make it a sub ingredient list


type RecipeNode struct {
  *goquery.Selection
}

func (s RecipeNode) ItemProp(elemName string, propName string) *goquery.Selection {
 return s.Find(elemName + "[itemprop=\"" + propName + "\"]")
}

func (s RecipeNode) ItemPropAttrOr(elemName string, propName string, attr string, defaultValue string) string {
 return s.ItemProp(elemName, propName).AttrOr(attr, defaultValue) 
}

func (s RecipeNode) ItemPropElemText(propName string) string {
 return s.ItemProp("", propName).Text() 
}

func (s RecipeNode) ItemPropContentOr(propName string, defaultValue string) string {
 return s.ItemPropAttrOr("meta", propName, "content", defaultValue)
}

func (s RecipeNode) ItemPropContentList(propName string) []string {
  contents := make([]string, 0)

  s.ItemProp("meta", propName).Each(func (i int, meta *goquery.Selection){
    content := meta.AttrOr("content", "") 
    if content != "" {
      contents = append(contents, content)
    }
  })

  return contents
}

var fractions = map[rune]string{
  '¼': "1/4",
  '½': "1/2",
  '¾': "3/4",
  '⅓': "1/3",
  '⅔': "2/3",
  '⅕': "1/5",
  '⅖': "2/5",
  '⅗': "3/5",
  '⅘': "4/5",
  '⅙': "1/6",
  '⅚': "5/6",
  '⅛': "1/8",
  '⅜': "3/8",
  '⅝': "5/8",
  '⅞': "7/8",
}

func ConvertFractions(input string) string {
	var output strings.Builder

	for _, r := range input {
		if replacement, exists := fractions[r]; exists {
			output.WriteString(replacement)
		} else {
			output.WriteRune(r)
		}
	}

	return output.String()
}

func (s RecipeNode) ItemPropChildrenText(propName string) []string {
  stringList := make([]string, 0)

  s.ItemProp("", propName).Children().Each(func (i int, par *goquery.Selection){
    partext := ConvertFractions(strings.TrimSpace(par.Text()))
    if partext != "" {
      stringList = append(stringList, partext)
    }
  })

  return stringList
}

func (s RecipeNode) ExtractRecipeCourses() []string {
  courses := make([]string, 0)

  s.ItemProp("", "recipeCourse").Each(func (i int, elem *goquery.Selection){
    // Courses can be split between a text node *and* attribute for extra courses
    if elem.Is("span") {
      course := elem.Text() 
      if course != "" {
        courses = append(courses, course)
      }
    } else if elem.Is("meta") {
      course := elem.AttrOr("content", "") 
      if course != "" {
        courses = append(courses, course)
      }
    }
  })

  return courses
}

func (s RecipeNode) ExtractRecipePhotos() []string {
  photos := make([]string, 0)

  s.Find("img.recipe-photos").Each(func (i int, img *goquery.Selection){
    img_src := img.AttrOr("src", "")
    if img_src != "" {
      photos = append(photos, img_src)
    }
  })

  return photos
}

func (s RecipeNode) ExtractRecipeMetadata() RecipeMetadata {
  metadata := RecipeMetadata{}

  metadata.UUID = s.ItemPropContentOr("recipeId", "")
	metadata.Favorited = s.ItemPropContentOr("recipeIsFavourite", "False") == "True"

  rating, err := strconv.Atoi(s.ItemPropContentOr("recipeRating", "0"))
  if err == nil { metadata.Rating = rating }

	metadata.Source = s.ItemPropElemText("recipeSource")

	metadata.CategoryList = s.ItemPropContentList("recipeCategory")
	metadata.CollectionList = s.ItemPropContentList("recipeCollection")
	metadata.CourseList = s.ExtractRecipeCourses()

	metadata.Yield = s.ItemPropElemText("recipeYield" )

	prepDuration, err := ParseISODuration(strings.TrimSpace(s.ItemPropContentOr("prepTime", "PT50S")))
	if err == nil { metadata.PrepTime = prepDuration }

	cookDuration, err := ParseISODuration(s.ItemPropContentOr("cookTime", "PT0S"))
	if err == nil { metadata.CookTime = cookDuration }

  return metadata
}

func (s RecipeNode) ExtractRecipeNutrition() RecipeNutrition {
  nutrition := RecipeNutrition{}

  nutrition.Serving = s.ItemPropContentOr("recipeNutServingSize", "")
  nutrition.Calories = s.ItemPropContentOr("recipeNutCalories", "") 
  nutrition.TotalFat = s.ItemPropContentOr("recipeNutTotalFat", "")
  nutrition.SaturatedFat = s.ItemPropContentOr("recipeNutSaturatedFat", "")
  nutrition.Sodium = s.ItemPropContentOr("recipeNutSodium", "")
  nutrition.TotalCarbohydrate = s.ItemPropContentOr("recipeNutTotalCarbohydrate", "")
  nutrition.DietaryFiber = s.ItemPropContentOr("recipeNutDietaryFiber", "")
  nutrition.Sugars = s.ItemPropContentOr("recipeNutSugars", "")
  nutrition.Protein = s.ItemPropContentOr("recipeNutProtein", "")

  return nutrition
}

func (s RecipeNode) ExtractRecipe() Recipe {
  recipe := Recipe{}

  recipe.Title = s.ItemPropElemText("name")
  recipe.Metadata = s.ExtractRecipeMetadata()
  recipe.PhotoPaths = s.ExtractRecipePhotos()

	recipe.IngredientLines = s.ItemPropChildrenText("recipeIngredients")
	recipe.InstructionLines = s.ItemPropChildrenText("recipeDirections")
	recipe.NotesLines = s.ItemPropChildrenText("recipeNotes")

  return recipe
}

type RecipeNutrition struct {
	Serving string
	Calories string
	TotalFat string
	SaturatedFat string
	Sodium string
	TotalCarbohydrate string
	DietaryFiber string
	Sugars string
	Protein string
}

type RecipeMetadata struct {
  UUID string
  Favorited bool
  Rating int
  Source string
  CategoryList []string
  CourseList []string
  CollectionList []string
  Yield string
  CookTime time.Duration
  PrepTime time.Duration
}

type Recipe struct {
  Title string
  Nutrition RecipeNutrition
  Metadata RecipeMetadata
  PhotoPaths []string
  IngredientLines []string
  InstructionLines []string
  NotesLines []string
}

func (r Recipe) FormatAsRecipeMD() string {
	var output strings.Builder
	output.WriteString(fmt.Sprintf("# %s\n", r.Title))

	output.WriteString("\n")
	if r.Metadata.Rating != 0 {
	  output.WriteString(fmt.Sprintf("Rating: %d-star\n", r.Metadata.Rating))
	}
	if len(r.Metadata.CollectionList) > 0 {
	  output.WriteString(fmt.Sprintf("Collections: %s\n", strings.Join(r.Metadata.CollectionList, ", ")))
	}
	if len(r.Metadata.CourseList) > 0 {
	  output.WriteString(fmt.Sprintf("Course: %s\n", strings.Join(r.Metadata.CourseList, ", ")))
	}

	output.WriteString("\n")
	if r.Metadata.Source != "" {
	  output.WriteString(fmt.Sprintf("Source: %s\n", r.Metadata.Source))
	}

	output.WriteString("\n")
	if r.Metadata.CookTime > time.Duration(0) {
	  output.WriteString(fmt.Sprintf("Cook Time: %s\n", r.Metadata.CookTime))
	}
	if r.Metadata.PrepTime > time.Duration(0) {
	  output.WriteString(fmt.Sprintf("Prep Time: %s\n", r.Metadata.PrepTime))
	}

	output.WriteString("\n")
	if len(r.Metadata.CategoryList) > 0 {
	  output.WriteString(fmt.Sprintf("*%s*\n", strings.Join(r.Metadata.CategoryList, ", ")))
	}

	output.WriteString("\n")
	if r.Metadata.Yield != "" {
	  output.WriteString(fmt.Sprintf("**%s**\n", r.Metadata.Yield))
	}

	output.WriteString("\n---\n\n")

	for _, ingredient := range r.IngredientLines {
	  output.WriteString(fmt.Sprintf("- %s\n", ingredient)) // TODO: parse so the ammount and unit go inside *
	}

	output.WriteString("\n---\n\n")

	output.WriteString("### Instructions\n\n")
	output.WriteString(strings.Join(r.InstructionLines, "\n"))

  if len(r.NotesLines) > 0 {
	  output.WriteString("\n\n### Notes\n\n")
	  output.WriteString(strings.Join(r.NotesLines, "\n"))
  }

	output.WriteString("\n")

	return output.String()
}

func (r Recipe) WriteRecipeMD() error {
	content := r.FormatAsRecipeMD()
	return os.WriteFile("./recipes/" + r.Metadata.UUID + ".md", []byte(content), 0644)
}

func ScrapeRecipeKeeperExportHtml(reader io.Reader) {
  doc, err := goquery.NewDocumentFromReader(reader)
  if err != nil {
    log.Fatal(err)
  }

  doc.Find("div.recipe-details").Each(func(i int, s *goquery.Selection) {

		r := RecipeNode{ s }
		recipe := r.ExtractRecipe()
		r.ExtractRecipe().WriteRecipeMD()

    if strings.Contains(recipe.Title, "Saag") {
    // if recipe.Metadata.Favorited {
		  // fmt.Printf("%+v\n", recipe)
		  // fmt.Print(r.ExtractRecipe().FormatAsRecipeMD())
    }
	})
}

func main() {
  path := "/home/kalebo/Downloads/RecipeKeeper_20230630_093852/recipes.html"

  file, err := os.Open(path)
  if err != nil {
    log.Fatal(err)
  }

  reader := io.Reader(file)
  defer file.Close()

  ScrapeRecipeKeeperExportHtml(reader)
}

// Dates are full of edge cases, and go has completely punted on ISO_8601 durations. :p
// So here we grab the following from https://gist.github.com/spatialtime/2a54a6dbf80121997b2459b2d3b9b380
func ParseISODuration(isoDuration string) (time.Duration, error) {
	re := regexp.MustCompile(`^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)D)?T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:.\d+)?)S)?$`)
	matches := re.FindStringSubmatch(isoDuration)
	if matches == nil {
		return 0, errors.New("input string is of incorrect format")
	}

	seconds := 0.0

	//skipping years and months

	//days
	if matches[3] != "" {
		f, err := strconv.ParseFloat(matches[3], 32)
		if err != nil {
			return 0, err
		}

		seconds += (f * 24 * 60 * 60)
	}
	//hours
	if matches[4] != "" {
		f, err := strconv.ParseFloat(matches[4], 32)
		if err != nil {
			return 0, err
		}

		seconds += (f * 60 * 60)
	}
	//minutes
	if matches[5] != "" {
		f, err := strconv.ParseFloat(matches[5], 32)
		if err != nil {
			return 0, err
		}

		seconds += (f * 60)
	}
	//seconds & milliseconds
	if matches[6] != "" {
		f, err := strconv.ParseFloat(matches[6], 32)
		if err != nil {
			return 0, err
		}

		seconds += f
	}

	goDuration := strconv.FormatFloat(seconds, 'f', -1, 32) + "s"
	return time.ParseDuration(goDuration)

}


